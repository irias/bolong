package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	remote = flag.String("remote", "", "remote location for backup files")
)

func check(err error, msg string) {
	if err == nil {
		return
	}
	if msg == "" {
		log.Fatal(err)
	}
	log.Fatalf("%s: %s\n", msg, err)
}

func main() {
	log.SetFlags(0)
	flag.Usage = func() {
		log.Println("usage:")
		log.Println("\tbackup -remote /path/to/storage backup")
		log.Println("\tbackup -remote /path/to/storage restore")
		log.Println("\tbackup -remote /path/to/storage list")
	}
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		os.Exit(1)
	}

	if *remote == "" {
		flag.Usage()
		os.Exit(1)
	}

	cmd := args[0]
	args = args[1:]
	switch cmd {
	case "backup":
		backup(args)
	case "restore":
		restore(args)
	case "list":
		list(args)
	default:
		flag.Usage()
		os.Exit(1)
	}
}

func backup(args []string) {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	incremental := fs.Bool("incremental", false, "do incremental backup instead of full backup")
	err := fs.Parse(args)
	if err != nil {
		log.Println(err)
		fs.Usage()
		os.Exit(2)
	}
	args = fs.Args()
	if len(args) != 1 {
		fs.Usage()
		os.Exit(2)
	}

	log.Println("backuping up", args[0])

	dir := args[0]
	info, err := os.Stat(dir)
	check(err, "stat backup dir")
	if !info.IsDir() {
		log.Fatal("can only backup directories")
	}
	if dir == "." {
		dir, err = os.Getwd()
		check(err, `resolving "."`)
	}
	if !strings.HasSuffix(dir, "/") {
		dir += "/"
	}

	nidx := &Index{}
	var oidx *Index
	unseen := map[string]*File{}
	if *incremental {
		backups, err := listBackups()
		check(err, "listing backups")
		if len(backups) == 0 {
			log.Fatal("no previous backups")
		}
		b := backups[len(backups)-1]
		nidx.previousName = b.name
		oidx, err = readIndex(b)
		check(err, "parsing previous index file")
		for _, f := range oidx.contents {
			unseen[f.name] = f
		}
	}

	name := time.Now().Format("20060201-150405")
	dataPath := fmt.Sprintf("%s/%s.data", *remote, name)
	data, err := os.Create(dataPath)
	check(err, "creating data file")

	dataOffset := int64(0)
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Fatalf("error walking %s: %s\n", path, err)
		}
		if !strings.HasPrefix(path, dir) {
			log.Printf("path not prefixed by dir? path %s, dir %s\n", path, dir)
			return filepath.SkipDir
		}
		relpath := path[len(dir):]
		if relpath == "" {
			relpath = "."
		}

		size := int64(0)
		if !info.IsDir() {
			size = info.Size()
		}
		nf := &File{
			info.IsDir(),
			info.Mode() & os.ModePerm,
			info.ModTime(),
			size,
			"xuser",
			"xgroup",
			-1, // data offset
			relpath,
		}

		nidx.contents = append(nidx.contents, nf)

		if *incremental {
			of, ok := unseen[relpath]
			if ok {
				delete(unseen, relpath)
				if !fileChanged(of, nf) {
					return nil
				}
			} else {
				nidx.add = append(nidx.add, relpath)
			}
		}

		if !nf.isDir {
			err := store(path, nf.size, data)
			if err != nil {
				log.Fatalf("writing %s: %s\n", path, err)
			}
			nf.dataOffset = dataOffset
			dataOffset += nf.size
		}

		return nil
	})

	if *incremental {
		for _, f := range unseen {
			nidx.delete = append(nidx.delete, f.name)
		}
	}

	err = data.Close()
	check(err, "closing data file")

	kind := "full"
	if *incremental {
		kind = "incr"
	}
	indexPath := fmt.Sprintf("%s/%s.index.%s", *remote, name, kind)
	index, err := os.Create(indexPath)
	check(err, "creating index file")
	err = writeIndex(index, nidx)
	check(err, "writing index file")
	err = index.Close()
	check(err, "closing index file")

	log.Println("wrote new backup:", name)
}

func fileChanged(old, new *File) bool {
	if old.name != new.name {
		log.Fatalf("inconsistent fileChanged call, names don't match, %s != %s", old.name, new.name)
	}
	return old.isDir != new.isDir || old.size != new.size || old.mtime.Unix() != new.mtime.Unix() || old.permissions != new.permissions || old.user != new.user || old.group != new.group
}

func store(path string, size int64, data io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	n, err := io.Copy(data, f)
	if err != nil {
		f.Close()
		return err
	}
	if n != size {
		f.Close()
		return fmt.Errorf("expected to write %d bytes, only wrote %d", size, n)
	}
	return f.Close()
}

func restore(args []string) {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	err := fs.Parse(args)
	if err != nil {
		log.Println(err)
		fs.Usage()
		os.Exit(2)
	}
	args = fs.Args()
	if len(args) != 2 {
		fs.Usage()
		os.Exit(2)
	}

	log.Printf("restoring %s to %s\n", args[0], args[1])

	name := args[0]
	backups, err := findBackups(name)
	check(err, "finding backups")
	backup, backups := backups[0], backups[1:]
	idx, err := readIndex(*backup)
	check(err, "parsing index")

	need := map[string]struct{}{} // files we still need to restore
	for _, f := range idx.contents {
		need[f.name] = struct{}{}
	}

	target := args[1]
	err = os.MkdirAll(target, 0777)
	if err != nil && !os.IsExist(err) {
		log.Fatalln("creating destination directory:", err)
	}
	if target == "." {
		target, err = os.Getwd()
		check(err, `resolving "."`)
	}
	if !strings.HasSuffix(target, "/") {
		target += "/"
	}

	for {
		// figure out if this backup has files we still need
		var restores []*File
		for _, file := range idx.contents {
			if !file.isDir && file.dataOffset < 0 {
				// not in this backup
				continue
			}
			if _, ok := need[file.name]; ok {
				restores = append(restores, file)
				delete(need, file.name)
			}
		}

		if len(restores) > 0 {
			dataPath := fmt.Sprintf("%s/%s.data", *remote, backup.name)
			log.Println("opening data file", dataPath)
			data, err := os.Open(dataPath)
			check(err, "open data file")

			// xxx should sort the restores by dataOffset, then read through the data, then restores as the files come along
			for _, file := range restores {
				tpath := target + file.name
				if file.isDir {
					if file.name != "." {
						err = os.Mkdir(tpath, file.permissions)
						check(err, "restoring directory")
					}
					err = os.Chtimes(tpath, file.mtime, file.mtime)
					check(err, "setting mtime for restored directory")
					continue
				}

				f, err := os.Create(tpath)
				check(err, "restoring file")
				log.Printf("restoring file %s, dataOffset %d, size %d\n", file.name, file.dataOffset, file.size)
				_, err = data.Seek(file.dataOffset, 0)
				check(err, "seeking in data file")
				r := &io.LimitedReader{R: data, N: file.size}
				n, err := io.Copy(f, r)
				if n != file.size {
					log.Fatalf("short file contents for file %s: expected to write %d, but wrote %d", file.name, file.size, n)
				}
				check(err, "restoring contents of file")
				err = f.Close()
				check(err, "closing restored file")
				err = os.Chmod(tpath, file.permissions)
				check(err, "setting permisssions on restored file")
				err = os.Chtimes(tpath, file.mtime, file.mtime)
				check(err, "setting mtimd/atime on restored file")
			}

			err = data.Close()
			check(err, "closing data file")
		}

		if len(need) == 0 {
			break
		}
		if len(backups) == 0 {
			log.Fatalf("still need to restore files, but no more backups available")
		}

		backup = backups[0]
		backups = backups[1:]
		idx, err = readIndex(*backup)
		check(err, "parsing next index")
		log.Println("next backup loaded", backup.name, backup.incremental)
	}

	log.Println("restored")
}

func list(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	err := fs.Parse(args)
	if err != nil {
		log.Println(err)
		fs.Usage()
		os.Exit(2)
	}
	args = fs.Args()
	if len(args) != 0 {
		fs.Usage()
		os.Exit(2)
	}

	l, err := listBackups()
	check(err, "listing backups")
	for _, b := range l {
		fmt.Println(b.name)
	}
}
