package main

import (
	"io"
)

type writeCounter struct {
	f    io.WriteCloser
	size int64
}

var _ io.WriteCloser = &writeCounter{}

func (w *writeCounter) Write(buf []byte) (int, error) {
	n, err := w.f.Write(buf)
	if n > 0 {
		w.size += int64(n)
	}
	return n, err
}

func (w *writeCounter) Close() error {
	return w.f.Close()
}
