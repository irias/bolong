package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

func backupCmd(args []string, name string) {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	fs.Usage = func() {
		log.Println("usage: bolong [flags] backup [flags] [directory]")
		fs.PrintDefaults()
	}
	verbose := fs.Bool("verbose", false, "print files being backed up")
	fs.Parse(args)
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

	// incremental backups list the previous incr/full backups that need files from
	// so we have to do some bookkeeping when we do an incremental backup, only keeping index files of previous backups that still have a file we need.
	type earlier struct {
		prev previous
		used bool
	}
	var earliers []earlier

	nidx := &index{}
	var oidx *index
	unseen := map[string]*file{}
	incremental := false
	if config.IncrementalsPerFull > 0 {
		// backups will be all incremental backups (most recent first), leading to the first full backup (also included)
		backups, err := findBackupChain("latest")
		if err == nil {
			incremental = len(backups)-1 < config.IncrementalsPerFull
			if incremental {
				b := backups[0]
				oidx, err = readIndex(b)
				check(err, "parsing previous index file")
				for _, f := range oidx.contents {
					unseen[f.name] = f
				}

				earliers = make([]earlier, len(oidx.previous)+1)
				for i, p := range oidx.previous {
					earliers[i] = earlier{p, false}
				}
				earliers[len(earliers)-1] = earlier{previous{true, b.name, oidx.dataSize}, false}
			}
		} else if err == errNotFound {
			// do first full
		} else {
			log.Fatalln("listing backups for determing full or incremental backup:", err)
		}
	}

	// keep track of the paths we've created at remote, so we can clean up them up when we are interrupted.
	partialpaths := make(chan string)
	cleanup := make(chan os.Signal)
	signal.Notify(cleanup, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		var paths []string
		cleaning := false
		for {
			select {
			case path := <-partialpaths:
				if path == "" {
					paths = nil
				} else {
					paths = append(paths, path)
				}
			case <-cleanup:
				if cleaning {
					log.Println("signal while cleaning up, quitting")
					os.Exit(1)
				}
				cleaning = true
				done := make(chan struct{})
				for _, path := range paths {
					go func(path string) {
						log.Println("cleaning up remote path", path)
						err := remote.Delete(path)
						if err != nil {
							log.Println("failed to cleanup remote path:", err)
						}
						done <- struct{}{}
					}(path)
				}
				for _ = range paths {
					<-done
				}
				os.Exit(1)
			}
		}
	}()

	dataPath := fmt.Sprintf("%s.data", name)
	var data io.WriteCloser
	data, err = remote.Create(dataPath)
	check(err, "creating data file")
	partialpaths <- dataPath
	dwc := &writeCounter{f: data}
	data = dwc
	data, err = newSafeWriter(data)
	check(err, "creating safe file")

	var whitelist []string // whitelisted directories. all children files will be included.
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
			match := matchAny(includes, matchPath)
			if match && info.IsDir() {
				whitelist = append(whitelist, matchPath)
			}
			if !match && !info.IsDir() {
				keep := false
				for _, white := range whitelist {
					if strings.HasPrefix(matchPath, white) {
						keep = true
						break
					}
				}
				if !keep {
					if *verbose {
						log.Println(`no "include" match, skipping`, matchPath)
					}
					return nil
				}
			}
		}
		if len(excludes) > 0 {
			match := matchAny(excludes, matchPath)
			if match {
				if *verbose {
					log.Println(`"exclude" match, skipping`, matchPath)
				}
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		size := int64(0)
		if !info.IsDir() {
			size = info.Size()
		}
		owner, group := userGroupName(info)
		nf := &file{
			info.IsDir(),
			info.Mode()&os.ModeSymlink != 0,
			info.Mode() & os.ModePerm,
			info.ModTime(),
			size,
			owner,
			group,
			-1, // data offset
			-1, // previous index, possibly updated later
			relpath,
		}

		nidx.contents = append(nidx.contents, nf)
		nfiles++

		if incremental {
			of, ok := unseen[relpath]
			if ok {
				delete(unseen, relpath)
				if !fileChanged(of, nf) {
					if !nf.isDir {
						nf.dataOffset = of.dataOffset
						// these indices are against the index file from the previous incremental backup.
						// we fix up these indices later on, after we know which previous backups are still referenced.
						prevIndex := of.previousIndex
						if prevIndex == -1 {
							// files contained in the last index are now in the new previous-index-reference
							prevIndex = len(earliers) - 1
						}
						nf.previousIndex = prevIndex
						earliers[prevIndex].used = true
					}
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

		if nf.isDir {
			return nil
		}
		nf.dataOffset = dataOffset
		if nf.isSymlink {
			p, err := os.Readlink(path)
			check(err, "readlink")
			buf := []byte(p)
			n, err := data.Write(buf)
			check(err, "write symlink data")
			if n != len(buf) {
				panic("did not write full buf")
			}
			nf.size = int64(n)
		} else {
			err := store(path, nf.size, data)
			if err != nil {
				log.Fatalf("writing %s: %s\n", path, err)
			}
		}
		dataOffset += nf.size

		return nil
	})

	if incremental {
		// map previousIndex from last index file to those in index file we're making now.
		// some old index/data files might may no longer be necessary, because all files contained within have been overwritten/deleted.
		prevIndexMap := map[int]int{}
		for i, e := range earliers {
			if e.used {
				prevIndexMap[i] = len(nidx.previous)
				nidx.previous = append(nidx.previous, e.prev)
			}
		}
		for _, f := range nidx.contents {
			if f.previousIndex >= 0 {
				f.previousIndex = prevIndexMap[f.previousIndex]
			}
		}
		for _, f := range unseen {
			nidx.delete = append(nidx.delete, f.name)
		}
	}

	err = data.Close()
	check(err, "closing data file")

	nidx.dataSize = dwc.size

	kind := "full"
	kindName := "full"
	if incremental {
		kind = "incr"
		kindName = "incremental"
	}
	indexPath := fmt.Sprintf("%s.index1.%s", name, kind)
	var index io.WriteCloser
	index, err = remote.Create(indexPath + ".tmp")
	check(err, "creating index file")
	partialpaths <- indexPath + ".tmp"
	index, err = newSafeWriter(index)
	iwc := &writeCounter{f: index}
	index = iwc
	check(err, "creating safe file")
	err = writeIndex(index, nidx)
	check(err, "writing index file")
	err = index.Close()
	check(err, "closing index file")
	err = remote.Rename(indexPath+".tmp", indexPath)
	check(err, "moving temp index file into place")
	partialpaths <- "" // signal that we're done

	if *verbose {
		log.Printf("new %s backup: %s\n", kindName, name)
		addDel := ""
		if incremental {
			addDel = fmt.Sprintf(", +%d files, -%d files", len(nidx.add), len(nidx.delete))
		}
		log.Printf("total files %d, total size %s, backup size %s%s\n", nfiles, formatSize(dataOffset), formatSize(dwc.size+iwc.size), addDel)
	}

	if config.FullKeep > 0 || config.IncrementalForFullKeep > 0 {
		backups, err := listBackups()
		check(err, "listing backups for cleaning up old backups")

		// cleanup full backups, and everything before that
		fullSeen := 0
		for i := len(backups) - 1; i > 0 && config.FullKeep > 0; i-- {
			if backups[i].incremental {
				continue
			}
			fullSeen++
			if fullSeen < config.FullKeep {
				continue
			}
			for j := 0; j < i; j++ {
				if *verbose {
					log.Println("cleaning up old backup", backups[j].name)
				}
				err = remote.Delete(backups[j].name + ".data")
				if err != nil {
					log.Println("removing old backup:", err)
				}
				ext := "full"
				if backups[j].incremental {
					ext = "incr"
				}
				err = remote.Delete(backups[j].name + ".index1." + ext)
				if err != nil {
					log.Println("removing old backup:", err)
				}
			}
			backups = backups[:i+1]
			break
		}

		fullSeen = 0
		for i := len(backups) - 1; i > 0; i-- {
			if backups[i].incremental {
				continue
			}
			fullSeen++
			if fullSeen < config.IncrementalForFullKeep {
				continue
			}
			for j := 0; j < i; j++ {
				if !backups[j].incremental {
					continue
				}
				if *verbose {
					log.Println("cleaning up old incremental backup", backups[j].name)
				}
				err = remote.Delete(backups[j].name + ".data")
				if err != nil {
					log.Println("removing old incremental backup:", err)
				}
				err = remote.Delete(backups[j].name + ".index1.incr")
				if err != nil {
					log.Println("removing old incremental backup:", err)
				}
			}
			break
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

func fileChanged(old, new *file) bool {
	if old.name != new.name {
		log.Fatalf("inconsistent fileChanged call, names don't match, %s != %s", old.name, new.name)
	}
	return old.isDir != new.isDir || old.isSymlink != new.isSymlink || old.size != new.size || old.mtime.Unix() != new.mtime.Unix() || old.permissions != new.permissions || old.user != new.user || old.group != new.group
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
