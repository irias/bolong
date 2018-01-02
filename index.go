package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

/*
example index file:

index1
1231823u123
f 20170101-122334 23423423423
i 20170102-122334 13144534
i 20170103-122334 2423422
- path/removed
+ path/to/file
= d 755 1506578834 0 mjl mjl 0 -1 path/to
= f 644 1506578834 1234 mjl mjl 0 1 path/to/file
= f 644 1506578834 100 mjl mjl 0 0 path/to/another-file
= f 644 1506578834 123123123 mjl mjl 100 0 path/to/another-file
= f 644 1506578834 23424 mjl mjl 100 -1 path/to/new/file
= s 644 1506578834 23424 mjl mjl 100 -1 path/to/new/symlink
.

*/

type index struct {
	dataSize int64
	previous []previous
	add      []string
	delete   []string
	contents []*file
}

type file struct {
	isDir         bool
	isSymlink     bool
	permissions   os.FileMode
	mtime         time.Time
	size          int64
	user          string
	group         string
	dataOffset    int64
	previousIndex int
	name          string
}

type previous struct {
	incremental bool
	name        string
	dataSize    int64 // size of data file, after compression and encryption
}

func (p previous) indexString() string {
	kind := "f"
	if p.incremental {
		kind = "i"
	}
	return fmt.Sprintf("%s %s %d", kind, p.name, p.dataSize)
}

func parsePrevious(s string) (p previous, err error) {
	t := strings.Split(s, " ")
	if len(t) != 3 {
		err = fmt.Errorf("bad number of tokens for previous line, got 3, expected %d", len(t))
		return
	}
	switch t[0] {
	default:
		err = fmt.Errorf(`bad kind "%s" for previous, must be "i" or "f"`, t[0])
		return
	case "i":
		p.incremental = true
	case "f":
		p.incremental = false
	}
	p.name = t[1]
	p.dataSize, err = strconv.ParseInt(t[2], 10, 64)
	if err != nil {
		err = fmt.Errorf(`bad size "%s" in previous: %s`, t[2], err)
	}
	return
}

func parseFile(nprevious int, line string) (*file, error) {
	t := strings.SplitN(line, " ", 9)
	if len(t) != 9 {
		return nil, fmt.Errorf("invalid file line, doesn't have 9 tokens: %s", line)
	}
	perm0, err := strconv.ParseInt(t[1], 8, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid permissions %s: %s", t[1], err)
	}
	f := &file{}
	switch t[0] {
	case "s":
		f.isSymlink = true
	case "f":
	case "d":
		f.isDir = true
	default:
		return nil, fmt.Errorf("invalid file type %s", t[0])
	}
	f.permissions = os.FileMode(perm0)

	mtime, err := strconv.ParseInt(t[2], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid mtime %s: %s", t[2], err)
	}
	f.mtime = time.Unix(mtime, 0)
	f.size, err = strconv.ParseInt(t[3], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid size %s: %s", t[3], err)
	}
	if f.size < 0 {
		return nil, fmt.Errorf("invalid size %s: %s", t[3], err)
	}
	f.user = t[4]
	f.group = t[5]
	f.dataOffset, err = strconv.ParseInt(t[6], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid offset %s: %s", t[6], err)
	}
	if f.dataOffset < 0 && f.dataOffset != -1 {
		return nil, fmt.Errorf("invalid offset %s: %s", t[6], err)
	}
	err = verifyPath(t[7])
	if err != nil {
		return nil, err
	}
	f.previousIndex, err = strconv.Atoi(t[7])
	if err != nil {
		return nil, fmt.Errorf("invalid previousIndex %s: %s", t[7], err)
	}
	if f.previousIndex >= nprevious {
		return nil, fmt.Errorf("previousIndex invalid")
	}
	f.name = t[8]
	return f, nil
}

func (f file) indexString() string {
	kind := "f"
	if f.isDir {
		kind = "d"
	} else if f.isSymlink {
		kind = "s"
	}
	return fmt.Sprintf("%s %o %d %d %s %s %d %d %s", kind, f.permissions, f.mtime.Unix(), f.size, f.user, f.group, f.dataOffset, f.previousIndex, f.name)
}

func readIndex(b *backup) (idx *index, err error) {
	kindName := "full"
	if b.incremental {
		kindName = "incr"
	}
	path := fmt.Sprintf("%s.index1.%s", b.name, kindName)
	var f io.ReadCloser
	f, err = remote.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open index file: %s", err)
	}
	tmpf, err := newSafeReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	f = tmpf
	defer func() {
		nerr := f.Close()
		if err == nil && nerr != nil {
			err = fmt.Errorf("closing index file: %s", err)
			idx = nil
		}
		return
	}()

	idx = &index{}

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return nil, fmt.Errorf("reading index file: %s", scanner.Err())
	}
	if scanner.Text() != "index1" {
		return nil, fmt.Errorf(`first line of index file not magic "index1"`)
	}
	if !scanner.Scan() {
		return nil, fmt.Errorf("no size line in index file")
	}
	idx.dataSize, err = strconv.ParseInt(scanner.Text(), 10, 64)
	if err != nil {
		return nil, fmt.Errorf(`invalid size "%s" in index file`, scanner.Text())
	}
	for {
		if !scanner.Scan() {
			return nil, fmt.Errorf("unexpected end of index: %s", scanner.Err())
		}
		line := scanner.Text()
		if line == "" {
			return nil, fmt.Errorf("empty line in index file")
		}
		if line == "." {
			break
		}
		if strings.HasPrefix(line, "f ") || strings.HasPrefix(line, "i ") {
			p, err := parsePrevious(line)
			if err != nil {
				return nil, fmt.Errorf("parsing previous-line: %s", err)
			}
			if !p.incremental && len(idx.previous) > 0 {
				return nil, fmt.Errorf("non-first can only be incremental backups")
			}
			idx.previous = append(idx.previous, p)
		} else if strings.HasPrefix(line, "- ") {
			idx.delete = append(idx.delete, line[2:])
		} else if strings.HasPrefix(line, "+ ") {
			idx.add = append(idx.add, line[2:])
		} else if strings.HasPrefix(line, "= ") {
			file, err := parseFile(len(idx.previous), line[2:])
			if err != nil {
				return nil, fmt.Errorf("parsing file-line: %s", err)
			}
			idx.contents = append(idx.contents, file)
		}
	}
	if scanner.Scan() {
		return nil, fmt.Errorf("data after closing dot")
	}
	return idx, scanner.Err()
}

func writeIndex(index io.Writer, idx *index) error {
	var xerr error
	handle := func(n int, err error) {
		if xerr == nil {
			xerr = err
		}
	}
	handle(fmt.Fprintf(index, "index1\n%d\n", idx.dataSize))
	for _, p := range idx.previous {
		handle(fmt.Fprintf(index, "%s\n", p.indexString()))
	}
	for _, name := range idx.add {
		handle(fmt.Fprintf(index, "+ %s\n", name))
	}
	for _, name := range idx.delete {
		handle(fmt.Fprintf(index, "- %s\n", name))
	}
	for _, f := range idx.contents {
		handle(fmt.Fprintf(index, "= %s\n", f.indexString()))
	}
	handle(fmt.Fprintf(index, ".\n"))
	return xerr
}

func verifyPath(path string) error {
	if path == "." {
		return nil
	}
	if path == "" {
		return fmt.Errorf("invalid path, it is empty")
	}
	if strings.HasPrefix(path, "/") {
		return fmt.Errorf("path invalid, starts with /")
	}
	t := strings.Split(path, "/")
	for _, elem := range t {
		if elem == "." {
			return fmt.Errorf(`path invalid, contains needless "."`)
		}
		if elem == ".." {
			return fmt.Errorf(`path invalid, contains ".."`)
		}
		if elem == "" {
			return fmt.Errorf(`path invalid, contains empty elements, eg "//"`)
		}
	}
	return nil
}
