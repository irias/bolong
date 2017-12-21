package main

import (
	"io"
)

type destination interface {
	List() (names []string, err error)
	Open(path string) (r io.ReadCloser, err error)
	Create(path string) (w io.WriteCloser, err error)
	Rename(opath, npath string) (err error)
	Delete(path string) (err error)
}
