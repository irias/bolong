package main

import (
	"errors"
	"fmt"
	"strings"
)

var (
	errNotFound = errors.New("not found")
)

type Backup struct {
	name        string
	incremental bool
}

// return backups in order of timestamp
func listBackups() ([]*Backup, error) {
	var r []*Backup
	l, err := remote.List()
	if err != nil {
		return nil, fmt.Errorf("listing remote: %s", err)
	}
	for _, name := range l {
		if strings.HasSuffix(name, ".index.full") {
			r = append(r, &Backup{name[:len(name)-len(".index.full")], false})
		}
		if strings.HasSuffix(name, ".index.incr") {
			r = append(r, &Backup{name[:len(name)-len(".index.full")], true})
		}
	}
	return r, nil
}

func findBackup(name string) (*Backup, error) {
	l, err := listBackups()
	if err != nil {
		return nil, fmt.Errorf("listing backups: %s", err)
	}
	for _, b := range l {
		if b.name == name {
			return b, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

// find the backup, and its predecessors, up until the first full backup
func findBackups(name string) ([]*Backup, error) {
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
			r := make([]*Backup, 0, i+1-lastFull)
			for j := i; j >= lastFull; j-- {
				r = append(r, l[j])
			}
			return r, nil
		}
	}
	return nil, errNotFound
}
