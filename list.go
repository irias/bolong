package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
)

var (
	errNotFound = errors.New("not found")
)

type backup struct {
	name        string
	incremental bool
}

// return backups in order of timestamp
func listBackups() ([]*backup, error) {
	var r []*backup
	l, err := remote.List()
	if err != nil {
		return nil, fmt.Errorf("listing remote: %s", err)
	}
	for _, name := range l {
		if strings.HasSuffix(name, ".index1.full") {
			r = append(r, &backup{name[:len(name)-len(".index1.full")], false})
		}
		if strings.HasSuffix(name, ".index1.incr") {
			r = append(r, &backup{name[:len(name)-len(".index1.incr")], true})
		}
	}
	return r, nil
}

func findBackup(name string) (*backup, error) {
	l, err := listBackups()
	if err != nil {
		return nil, fmt.Errorf("listing backups: %s", err)
	}
	if name == "latest" && len(l) > 0 {
		return l[len(l)-1], nil
	}
	for _, b := range l {
		if b.name == name {
			return b, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

// find the backup, and its predecessors, up until the first full backup
func findBackupChain(name string) ([]*backup, error) {
	l, err := listBackups()
	if err != nil {
		return nil, fmt.Errorf("listing backups: %s", err)
	}
	lastFull := -1
	for i, b := range l {
		if !b.incremental {
			lastFull = i
		}
		if b.name == name || (name == "latest" && i == len(l)-1) {
			r := make([]*backup, 0, i+1-lastFull)
			for j := i; j >= lastFull; j-- {
				r = append(r, l[j])
			}
			return r, nil
		}
	}
	return nil, errNotFound
}

func list(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	fs.Usage = func() {
		log.Println("usage: bolong [flags] list")
		fs.PrintDefaults()
	}
	fs.Parse(args)
	args = fs.Args()
	if len(args) != 0 {
		fs.Usage()
		os.Exit(2)
	}

	l, err := listBackups()
	check(err, "listing backups")
	for _, b := range l {
		kind := "full"
		if b.incremental {
			kind = "incr"
		}
		fmt.Println(b.name, kind)
	}
}
