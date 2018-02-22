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

func (b backup) GoString() string {
	return fmt.Sprintf("backup{name: %s, incremental: %v}", b.name, b.incremental)
}

// return backups in order of timestamp
func listBackups() ([]*backup, error) {
	var r []*backup
	l, err := store.List()
	if err != nil {
		return nil, fmt.Errorf("listing remote: %s", err)
	}
	const (
		fullSuffix = ".index1.full"
		incrSuffix = ".index1.incr"
	)
	for _, name := range l {

		if strings.HasSuffix(name, fullSuffix) {
			b := &backup{
				name:        name[:len(name)-len(fullSuffix)],
				incremental: false,
			}
			r = append(r, b)
		}
		if strings.HasSuffix(name, incrSuffix) {
			b := &backup{
				name:        name[:len(name)-len(incrSuffix)],
				incremental: true,
			}
			r = append(r, b)
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
