package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
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

	name := time.Now().Format("20060201-150405")
	indexPath := fmt.Sprintf("%s/%s.full.index", *remote, name)
	dataPath := fmt.Sprintf("%s/%s.full.data", *remote, name)
	index, err := os.Create(indexPath)
	check(err, "creating index file")
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
		if info.IsDir() {
			_, err = fmt.Fprintf(index, "%s d %o %d 0 %s %s 0\n", relpath, info.Mode()&os.ModePerm, info.ModTime().Unix(), "xxx", "xxx")
			check(err, "writing to index")
		} else {
			size, err := store(path, data)
			if err != nil {
				log.Fatalf("writing %s: %s\n", path, err)
			}
			_, err = fmt.Fprintf(index, "%s f %o %d %d %s %s %d\n", relpath, info.Mode()&os.ModePerm, info.ModTime().Unix(), size, "xxx", "xxx", dataOffset)
			if err != nil {
				log.Fatalf("writing %s: %s\n", path, err)
			}
			dataOffset += size
		}
		return nil
	})

	err = data.Close()
	check(err, "closing data file")
	err = index.Close()
	check(err, "closing index file")
	log.Println("wrote new backup:", name)
}

func store(path string, data io.Writer) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return -1, err
	}
	size, err := io.Copy(data, f)
	if err != nil {
		f.Close()
		return -1, err
	}
	return size, f.Close()
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
	indexPath := fmt.Sprintf("%s/%s.full.index", *remote, name)
	index, err := os.Open(indexPath)
	check(err, "open index file")
	dataPath := fmt.Sprintf("%s/%s.full.data", *remote, name)
	data, err := os.Open(dataPath)
	check(err, "open data file")

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

	lines := bufio.NewScanner(index)
	for lines.Scan() {
		t := strings.Split(lines.Text(), " ") // xxx should handle spaces in paths!
		if len(t) != 8 {
			log.Fatalf("invalid line, doesn't have 8 tokens: %s\n", lines.Text())
		}
		verifyPath(t[0])
		perm0, err := strconv.ParseInt(t[2], 8, 64)
		if err != nil {
			log.Fatalf("invalid permission %s: %s\n", t[2], err)
		}
		perm := os.FileMode(perm0)

		mtime, err := strconv.ParseInt(t[3], 10, 64)
		if err != nil {
			log.Fatalf("invalid mtime %s: %s\n", err)
		}
		mtm := time.Unix(mtime, 0)
		size, err := strconv.ParseInt(t[4], 10, 64)
		if err != nil {
			log.Fatalf("invalid size %s: %s\n", err)
		}
		offset, err := strconv.ParseInt(t[7], 10, 64)
		if err != nil {
			log.Fatalf("invalid offset %s: %s\n", err)
		}
		tpath := target + t[0]

		switch t[1] {
		case "f":
			f, err := os.Create(tpath)
			check(err, "restoring file")
			_, err = data.Seek(offset, 0)
			check(err, "seeking in data file")
			r := &io.LimitedReader{R: f, N: size}
			_, err = io.Copy(f, r)
			check(err, "restoring contents of file")
			err = f.Close()
			check(err, "closing restored file")
			err = os.Chmod(tpath, perm)
			check(err, "setting permisssions on restored file")
			err = os.Chtimes(tpath, mtm, mtm)
			check(err, "setting mtimd/atime on restored file")

		case "d":
			if t[0] != "." {
				err = os.Mkdir(tpath, perm)
				check(err, "restoring directory")
			}
			err = os.Chtimes(tpath, mtm, mtm)
			check(err, "setting mtime for restored directory")
		default:
			log.Fatalln("unknown file type:", tpath)
		}
	}

	err = data.Close()
	check(err, "closing data file")
	err = index.Close()
	check(err, "closing index file")
	log.Println("restored")
}

func verifyPath(path string) {
	if path == "." {
		return
	}
	if path == "" {
		log.Fatal("invalid path, it is empty")
	}
	if strings.HasPrefix(path, "/") {
		log.Fatal("path invalid, starts with /")
	}
	t := strings.Split(path, "/")
	for _, elem := range t {
		if elem == "." {
			log.Fatal(`path invalid, contains needless "."`)
		}
		if elem == ".." {
			log.Fatal(`path invalid, contains ".."`)
		}
		if elem == "" {
			log.Fatal(`path invalid, contains empty elements, eg "//"`)
		}
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

	l, err := ioutil.ReadDir(*remote)
	check(err, "reading remote directory")
	for _, info := range l {
		name := info.Name()
		if strings.HasSuffix(name, ".full.index") {
			fmt.Println(name[:len(name)-len(".full.index")])
		}
		if strings.HasSuffix(name, ".incr.index") {
			fmt.Println(name[:len(name)-len(".incr.index")])
		}
	}
}
