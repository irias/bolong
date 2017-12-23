package main

import (
	"io"
)

type readCounter struct {
	f io.ReadCloser
	c chan int
}

var _ io.ReadCloser = &readCounter{}

func (r *readCounter) Read(buf []byte) (int, error) {
	n, err := r.f.Read(buf)
	if n > 0 {
		r.c <- n
	}
	return n, err
}

func (r *readCounter) Close() error {
	return r.f.Close()
}
