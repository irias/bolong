package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
)

func restore(args []string) {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	fs.Usage = func() {
		log.Println("usage: bolong [flags] restore [flags] destination [path-regepx ...]")
		fs.PrintDefaults()
	}
	verbose := fs.Bool("verbose", false, "print restored files")
	name := fs.String("name", "latest", "name of backup to restore")
	err := fs.Parse(args)
	if err != nil {
		log.Println(err)
		fs.Usage()
		os.Exit(2)
	}
	args = fs.Args()
	if len(args) == 0 {
		fs.Usage()
		os.Exit(2)
	}
	target := args[0]
	regexps := []*regexp.Regexp{}
	for _, pattern := range args[1:] {
		re, err := regexp.Compile(pattern)
		if err != nil {
			log.Fatalf("compiling regexp %s: %s\n", pattern, err)
		}
		regexps = append(regexps, re)
	}

	log.Printf("restoring %s to %s\n", *name, target)

	var backups []*Backup
	if *name == "latest" {
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
		backups, err = findBackups(*name)
		check(err, "finding backups")
	}
	backup, backups := backups[0], backups[1:]
	idx, err := readIndex(backup)
	check(err, "parsing index")

	need := map[string]struct{}{} // files we still need to restore
	for _, f := range idx.contents {
		if len(regexps) > 0 && !matchAny(regexps, f.name) {
			continue
		}
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
		if *verbose {
			log.Println("next incremental backup loaded", backup.name, backup.incremental)
		}
	}
}
