package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func backup(args []string) {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	fs.Usage = func() {
		log.Println("usage: bolong [flags] backup [flags] directory")
		fs.PrintDefaults()
	}
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

	includes := []*regexp.Regexp{}
	for _, s := range config.Include {
		re, err := regexp.Compile(s)
		if err != nil {
			log.Fatalf("bad include regexp %s: %s", s, err)
		}
		includes = append(includes, re)
	}
	excludes := []*regexp.Regexp{}
	for _, s := range config.Exclude {
		re, err := regexp.Compile(s)
		if err != nil {
			log.Fatalf("bad exclude regexp %s: %s", s, err)
		}
		excludes = append(excludes, re)
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
	incremental := false
	if config.IncrementalsPerFull > 0 {
		// backups will be all incremental backups (most recent first), leading to the first full backup (also included)
		backups, err := findBackups("latest")
		if err == nil {
			incremental = len(backups)-1 < config.IncrementalsPerFull
			b := backups[0]
			nidx.previousName = b.name
			oidx, err = readIndex(b)
			check(err, "parsing previous index file")
			for _, f := range oidx.contents {
				unseen[f.name] = f
			}
		} else if err == errNotFound {
			// do first full
		} else {
			log.Fatalln("listing backups for determing full or incremental backup:", err)
		}
	}

	name := time.Now().UTC().Format("20060102-150405")
	dataPath := fmt.Sprintf("%s.data", name)
	var data io.WriteCloser
	data, err = remote.Create(dataPath)
	check(err, "creating data file")
	data, err = NewSafeWriter(data)
	check(err, "creating safe file")

	dataOffset := int64(0)
	nfiles := 0
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Fatalf("error walking %s: %s\n", path, err)
		}
		if !strings.HasPrefix(path, dir) {
			log.Printf("path not prefixed by dir? path %s, dir %s\n", path, dir)
			return nil
		}
		relpath := path[len(dir):]
		matchPath := relpath
		if relpath == "" {
			relpath = "."
		}
		if relpath == ".bolong.json" || strings.HasSuffix(relpath, "/.bolong.json") {
			return nil
		}
		if info.IsDir() && matchPath != "" {
			matchPath += "/"
		}
		if len(includes) > 0 {
			if !matchAny(includes, matchPath) {
				if info.IsDir() {
					if *verbose {
						log.Println(`no "include" match, skipping`, matchPath)
					}
					return filepath.SkipDir
				}
				if *verbose {
					log.Println(`no "include" match, skipping`, matchPath)
				}
				return nil
			}
		}
		if len(excludes) > 0 {
			if matchAny(excludes, matchPath) {
				if info.IsDir() {
					if *verbose {
						log.Println(`"exclude" match, skipping`, matchPath)
					}
					return filepath.SkipDir
				}
				if *verbose {
					log.Println(`"exclude" match, skipping`, matchPath)
				}
				return nil
			}
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
		nfiles += 1

		if incremental {
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

	if incremental {
		for _, f := range unseen {
			nidx.delete = append(nidx.delete, f.name)
		}
	}

	err = data.Close()
	check(err, "closing data file")

	kind := "full"
	kindName := "full"
	if incremental {
		kind = "incr"
		kindName = "incremental"
	}
	indexPath := fmt.Sprintf("%s.index.%s", name, kind)
	var index io.WriteCloser
	index, err = remote.Create(indexPath + ".tmp")
	check(err, "creating index file")
	index, err = NewSafeWriter(index)
	check(err, "creating safe file")
	indexSize, err := writeIndex(index, nidx)
	check(err, "writing index file")
	err = index.Close()
	check(err, "closing index file")
	err = remote.Rename(indexPath+".tmp", indexPath)
	check(err, "moving temp index file into place")

	if *verbose {
		log.Printf("new %s backup: %s\n", kindName, name)
		addDel := ""
		if incremental {
			addDel = fmt.Sprintf(", +%d files, -%d files", len(nidx.add), len(nidx.delete))
		}
		totalSize := int64(indexSize) + dataOffset
		size := ""
		if totalSize > 1024*1024*1024 {
			size = fmt.Sprintf("%.1fgb", float64(totalSize)/(1024*1024*1024))
		} else {
			size = fmt.Sprintf("%.1fmb", float64(totalSize)/(1024*1024))
		}
		log.Printf("total size %s, total files %d%s\n", size, nfiles, addDel)
	}

	if config.FullKeep > 0 || config.IncrementalForFullKeep > 0 {
		backups, err := listBackups()
		check(err, "listing backups for cleaning up old backups")

		fullSeen := 0
		for i := len(backups) - 1; i > 0; i-- {
			if !backups[i].incremental {
				fullSeen += 1
			}
			if fullSeen >= config.IncrementalForFullKeep {
				for j := 0; j < i; j++ {
					if backups[j].incremental {
						if *verbose {
							log.Println("cleaning up old incremental backup", backups[j].name)
						}
						err = remote.Delete(backups[j].name + ".data")
						if err != nil {
							log.Println("removing old incremental backup:", err)
						}
						err = remote.Delete(backups[j].name + ".index.incr")
						if err != nil {
							log.Println("removing old incremental backup:", err)
						}
					}
				}
				break
			}
		}

		fullSeen = 0
		for i := len(backups) - 1; i > 0; i-- {
			if !backups[i].incremental {
				fullSeen += 1
			}
			if fullSeen >= config.FullKeep {
				for j := 0; j < i; j++ {
					if *verbose {
						log.Println("cleaning up old full backup", backups[j].name)
					}
					err = remote.Delete(backups[j].name + ".data")
					if err != nil {
						log.Println("removing old full backup:", err)
					}
					err = remote.Delete(backups[j].name + ".index.full")
					if err != nil {
						log.Println("removing old full backup:", err)
					}
				}
				break
			}
		}
	}
}

func matchAny(l []*regexp.Regexp, s string) bool {
	for _, re := range l {
		if re.FindStringIndex(s) != nil {
			return true
		}
	}
	return false
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
