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

type File struct {
	isDir       bool
	permissions os.FileMode
	mtime       time.Time
	size        int64
	user        string
	group       string
	dataOffset  int64
	name        string
}

func parseFile(line string) (*File, error) {
	t := strings.SplitN(line, " ", 8)
	if len(t) != 8 {
		return nil, fmt.Errorf("invalid file line, doesn't have 8 tokens: %s", line)
	}
	perm0, err := strconv.ParseInt(t[1], 8, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid permissions %s: %s", t[1], err)
	}
	f := &File{}
	switch t[0] {
	case "f":
		f.isDir = false
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
	f.name = t[7]
	return f, nil
}

func (f File) indexString() string {
	kind := "f"
	if f.isDir {
		kind = "d"
	}
	return fmt.Sprintf("%s %o %d %d %s %s %d %s", kind, f.permissions, f.mtime.Unix(), f.size, f.user, f.group, f.dataOffset, f.name)
}

type Index struct {
	previousName string
	add          []string
	delete       []string
	contents     []*File
}

func readIndex(b *Backup) (idx *Index, err error) {
	kindName := "full"
	if b.incremental {
		kindName = "incr"
	}
	path := fmt.Sprintf("%s/%s.index.%s", config.Remote, b.name, kindName)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open index file: %s", err)
	}
	defer func() {
		nerr := f.Close()
		if nerr != nil && err == nil {
			err = fmt.Errorf("closing index file: %s", err)
			idx = nil
		}
		return
	}()

	idx = &Index{}

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return nil, fmt.Errorf("empty index file")
	}
	if scanner.Text() != "index0" {
		return nil, fmt.Errorf(`first line of index file not magic "index0"`)
	}
	if !scanner.Scan() {
		return nil, fmt.Errorf(`no second line in index`)
	}
	idx.previousName = scanner.Text()
	for {
		if !scanner.Scan() {
			return nil, fmt.Errorf("unexpected end of index")
		}
		line := scanner.Text()
		if line == "" {
			return nil, fmt.Errorf("empty line in index file")
		}
		if line == "." {
			break
		}
		if strings.HasPrefix(line, "- ") {
			idx.delete = append(idx.delete, line[2:])
		} else if strings.HasPrefix(line, "+ ") {
			idx.add = append(idx.add, line[2:])
		} else if strings.HasPrefix(line, "= ") {
			file, err := parseFile(line[2:])
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

func writeIndex(index io.Writer, idx *Index) error {
	_, err := fmt.Fprintf(index, "index0\n%s\n", idx.previousName)
	if err != nil {
		return err
	}

	for _, name := range idx.add {
		_, err = fmt.Fprintf(index, "+ %s\n", name)
		if err != nil {
			return err
		}
	}
	for _, name := range idx.delete {
		_, err = fmt.Fprintf(index, "- %s\n", name)
		if err != nil {
			return err
		}
	}
	for _, f := range idx.contents {
		_, err = fmt.Fprintf(index, "= %s\n", f.indexString())
		if err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(index, ".\n")
	return err
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
