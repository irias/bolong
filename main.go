package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var (
	configPath = flag.String("config", "", "path to config file")
	config     struct {
		Kind     string
		Local    string
		GoogleS3 struct {
			AccessKey,
			Secret,
			Bucket,
			Path      string
		}
		Passphrase string
	}
	remote Remote
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
		log.Println("\tbackup [-config config.json] backup [flags] [directory]")
		log.Println("\tbackup [-config config.json] restore [flags] destination [backup-id]")
		log.Println("\tbackup [-config config.json] list")
	}
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		os.Exit(1)
	}

	if *configPath == "" {
		findConfigPath()
	}
	f, err := os.Open(*configPath)
	check(err, "opening config file")
	err = json.NewDecoder(f).Decode(&config)
	check(err, "parsing config file")

	switch config.Kind {
	case "local":
		if config.Local == "" {
			log.Fatal(`field "local" must be set for kind "local"`)
		}
		path := config.Local
		if !strings.HasSuffix(path, "/") {
			path += "/"
		}
		remote = &Local{path}
	case "googles3":
		if config.GoogleS3.AccessKey == "" || config.GoogleS3.Secret == "" || config.GoogleS3.Bucket == "" || config.GoogleS3.Path == "" {
			log.Fatal(`fields "googles3.accessKey", "googles3.secret", googles3.bucket" and  "googles3.path" must be set`)
		}
		path := config.GoogleS3.Path
		if !strings.HasPrefix(path, "/") || !strings.HasSuffix(path, "/") {
			log.Fatal(`field "googles3.path" must start and end with a slash`)
		}
		remote = &GoogleS3{config.GoogleS3.Bucket, path}
	case "":
		log.Fatal(`missing field "kind", must be "local" or "googles3"`)
	default:
		log.Fatalf(`unknown remote kind "%s"`, config.Kind)
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

func findConfigPath() {
	dir, err := os.Getwd()
	check(err, "looking for config file in current working directory")
	for {
		xpath := dir + "/.bolong.json"
		_, err := os.Stat(xpath)
		if err == nil {
			*configPath = xpath
			return
		}
		if !os.IsNotExist(err) {
			log.Fatal("cannot find a .bolong.json up in directory hierarchy")
		}
		ndir := path.Dir(dir)
		if ndir == dir {
			log.Fatal("cannot find a .bolong.json up in directory hierarchy")
		}
		dir = ndir
	}
}

func backup(args []string) {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	incremental := fs.Bool("incremental", false, "do incremental backup instead of full backup")
	verbose := fs.Bool("verbose", false, "print files being backed up")
	err := fs.Parse(args)
	if err != nil {
		log.Println(err)
		fs.Usage()
		os.Exit(2)
	}
	args = fs.Args()

	dir := "."
	switch len(args) {
	case 0:
	case 1:
		dir = args[0]
	default:
		fs.Usage()
		os.Exit(2)
	}

	if *verbose {
		log.Println("backuping up", dir)
	}

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

	name := time.Now().Format("20060102-150405")
	dataPath := fmt.Sprintf("%s.data", name)
	var data io.WriteCloser
	data, err = remote.Create(dataPath)
	check(err, "creating data file")
	data, err = NewSafeWriter(data)
	check(err, "creating safe file")

	dataOffset := int64(0)
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Fatalf("error walking %s: %s\n", path, err)
		}
		if !strings.HasPrefix(path, dir) {
			log.Printf("path not prefixed by dir? path %s, dir %s\n", path, dir)
			return nil
		}
		relpath := path[len(dir):]
		if relpath == "" {
			relpath = "."
		}
		if relpath == ".bolong.json" || strings.HasSuffix(relpath, "/.bolong.json") {
			return nil
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
				if *verbose {
					fmt.Println(relpath)
				}
			}
		} else {
			if *verbose {
				fmt.Println(relpath)
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
	indexPath := fmt.Sprintf("%s.index.%s", name, kind)
	var index io.WriteCloser
	index, err = remote.Create(indexPath)
	check(err, "creating index file")
	index, err = NewSafeWriter(index)
	check(err, "creating safe file")
	err = writeIndex(index, nidx)
	check(err, "writing index file")
	err = index.Close()
	check(err, "closing index file")

	if *verbose {
		log.Println("wrote new backup:", name)
	}
}

func fileChanged(old, new *File) bool {
	if old.name != new.name {
		log.Fatalf("inconsistent fileChanged call, names don't match, %s != %s", old.name, new.name)
	}
	return old.isDir != new.isDir || old.size != new.size || old.mtime.Unix() != new.mtime.Unix() || old.permissions != new.permissions || old.user != new.user || old.group != new.group
}

func store(path string, size int64, data io.Writer) (err error) {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		err2 := f.Close()
		if err == nil {
			err = err2
		}
	}()
	n, err := io.Copy(data, f)
	if err != nil {
		return err
	}
	if n != size {
		return fmt.Errorf("expected to write %d bytes, only wrote %d", size, n)
	}
	return
}

func restore(args []string) {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	verbose := fs.Bool("verbose", false, "print restored files")
	err := fs.Parse(args)
	if err != nil {
		log.Println(err)
		fs.Usage()
		os.Exit(2)
	}
	args = fs.Args()

	var target, name string
	switch len(args) {
	case 1:
		target = args[0]
		name = "latest"
	case 2:
		target = args[0]
		name = args[1]
	default:
		fs.Usage()
		os.Exit(2)
	}

	log.Printf("restoring %s to %s\n", name, target)

	var backups []*Backup
	if name == "latest" {
		backups, err = listBackups()
		check(err, "listing backups")
		if len(backups) == 0 {
			log.Fatal("no backups available")
		}
		var r []*Backup
		for i := len(backups) - 1; i >= 0; i-- {
			r = append(r, backups[i])
			if !backups[i].incremental {
				break
			}
		}
		backups = r
	} else {
		backups, err = findBackups(name)
		check(err, "finding backups")
	}
	backup, backups := backups[0], backups[1:]
	idx, err := readIndex(backup)
	check(err, "parsing index")

	need := map[string]struct{}{} // files we still need to restore
	for _, f := range idx.contents {
		need[f.name] = struct{}{}
	}

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
			dataPath := fmt.Sprintf("%s.data", backup.name)
			var data io.ReadCloser
			data, err := remote.Open(dataPath)
			check(err, "open data file")
			data, err = NewSafeReader(data)
			check(err, "opening safe reader")

			sort.Slice(restores, func(i, j int) bool {
				// make sure directories are created in right order
				if restores[i].dataOffset == restores[j].dataOffset {
					return restores[i].name < restores[j].name
				}
				return restores[i].dataOffset < restores[j].dataOffset
			})

			offset := int64(0)
			for _, file := range restores {
				if *verbose {
					fmt.Println(file.name)
				}
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

				if file.dataOffset > offset {
					_, err := io.Copy(ioutil.Discard, &io.LimitedReader{R: data, N: file.dataOffset - offset})
					check(err, "skipping through data")
					offset = file.dataOffset
				}

				f, err := os.Create(tpath)
				check(err, "restoring file")
				r := &io.LimitedReader{R: data, N: file.size}
				n, err := io.Copy(f, r)
				if n != file.size {
					log.Fatalf("short file contents for file %s: expected to write %d, but wrote %d", file.name, file.size, n)
				}
				offset += file.size
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
		idx, err = readIndex(backup)
		check(err, "parsing next index")
		log.Println("next backup loaded", backup.name, backup.incremental)
	}
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
