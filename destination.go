package main

import (
	"io"
)

type destination interface {
	// List returns the names of backup files, those ending in .full, in ascending order, by name, which is a timestamp.
	List() (names []string, err error)

	// open
	Open(path string) (r io.ReadCloser, err error)
	Create(path string) (w io.WriteCloser, err error)
	Rename(opath, npath string) (err error)
	Delete(path string) (err error)
}
