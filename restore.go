package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
)

type restore struct {
	previousIndex int
	previous      previous
	files         []*file // no directories
}

func restoreCmd(args []string) {
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

	backup, err := findBackup(*name)
	check(err, "looking up backup")

	idx, err := readIndex(backup)
	check(err, "parsing index")

	idx.previous = append(idx.previous, previous{backup.incremental, backup.name, idx.dataSize})
	var (
		restoreMap = map[int]*restore{}
		restores   []*restore
		dataSize   int64
		totalSize  int64
		nfiles     int
		dirs       []*file
		needDirs   = map[string]struct{}{}
	)
	for _, f := range idx.contents {
		if f.isDir {
			dirs = append(dirs, f)
		}
		if len(regexps) > 0 && !matchAny(regexps, f.name) {
			continue
		}
		if f.isDir {
			needDirs[f.name] = struct{}{}
			continue
		}

		dir := path.Dir(f.name)
		for {
			needDirs[dir] = struct{}{}
			if dir == "." {
				break
			}
			dir = path.Dir(dir)
		}

		prevIndex := f.previousIndex
		if prevIndex < 0 {
			prevIndex = len(idx.previous) - 1
		}
		rest, ok := restoreMap[prevIndex]
		if !ok {
			rest = &restore{prevIndex, idx.previous[prevIndex], nil}
			restoreMap[prevIndex] = rest
			restores = append(restores, rest)
			dataSize += rest.previous.dataSize
		}
		rest.files = append(rest.files, f)
		totalSize += f.size
		nfiles++
	}
	if *verbose {
		dirWord := "dirs"
		if len(dirs) == 1 {
			dirWord = "dir"
		}
		fileWord := "files"
		if nfiles == 1 {
			fileWord = "file"
		}
		partWord := "parts"
		if len(restores) == 1 {
			partWord = "part"
		}
		log.Printf("restoring %d %s and %d %s totalling %s which requires fetching %s for %d backup %s\n", len(dirs), dirWord, nfiles, fileWord, formatSize(totalSize), formatSize(dataSize), len(restores), partWord)
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

	restorePrevious := func(rest *restore) {
		dataPath := fmt.Sprintf("%s.data", rest.previous.name)
		var data io.ReadCloser
		data, err := remote.Open(dataPath)
		check(err, "open data file")
		data, err = newSafeReader(data)
		check(err, "opening safe reader")
		defer func() {
			err := data.Close()
			check(err, "closing data file")
		}()

		sort.Slice(rest.files, func(i, j int) bool {
			return rest.files[i].dataOffset < rest.files[j].dataOffset
		})

		offset := int64(0)
		for _, file := range rest.files {
			if *verbose {
				fmt.Println(file.name)
			}
			tpath := target + file.name

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
			check(err, "setting mtime/atime on restored file")
		}
	}

	// restore all directories first. ensures creating files always works.
	for _, f := range dirs {
		if _, ok := needDirs[f.name]; ok && f.name != "." {
			err = os.Mkdir(target+f.name, f.permissions)
			check(err, "restoring directory")
		}
	}

	// start restoring.
	// we restore 3 data files at a time, for higher throughput.
	// we start the first & last data files first. those are most likely to be big and dominate the time it takes to restore.
	workc := make(chan *restore, len(restores))
	donec := make(chan struct{}, 1)
	worker := func() {
		for {
			restorePrevious(<-workc)
			donec <- struct{}{}
		}
	}
	go worker()
	go worker()
	go worker()

	if len(restores) > 0 {
		workc <- restores[0]
	}
	if len(restores) > 1 {
		workc <- restores[len(restores)-1]
		for _, rest := range restores[1 : len(restores)-1] {
			workc <- rest
		}
	}
	for i := 0; i < len(restores); i++ {
		<-donec
	}

	// restore mtimes for directories
	for _, f := range dirs {
		if _, ok := needDirs[f.name]; ok {
			err = os.Chtimes(target+f.name, f.mtime, f.mtime)
			check(err, "setting mtime for restored directory")
		}
	}
}
