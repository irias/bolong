package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type testFile struct {
	path     string
	contents string
}

type testDir struct {
	path string
}

type testTree struct {
	files []testFile
	dirs  []testDir
}

func TestMain(t *testing.T) {
	// create config file, make a few backups, some full, some incremental.  with changed files.
	// test that all files are properly restored, that old incrementals are removed. the inclusion/exclusion.

	test := func(err error, msg string) {
		t.Helper()
		if err != nil {
			t.Errorf("%s: %s", msg, err)
		}
	}

	xremoveAll := func(path string) {
		t.Helper()
		err := os.RemoveAll(path)
		test(err, "removing tree")
	}

	xmkdirAll := func(path string) {
		t.Helper()
		err := os.MkdirAll(path, 0777)
		test(err, "making tree")
	}

	writeConfig := func() {
		c := configuration{
			Kind: "local",
			Include: []string{
				"\\.txt$",
				"^a/b/$",
			},
			Exclude: []string{
				"excluded",
			},
			IncrementalsPerFull:    2,
			FullKeep:               2,
			IncrementalForFullKeep: 1,
			Passphrase:             "test1234",
		}
		c.Local.Path = "testdir/backup"
		f, err := os.Create("testdir/workdir/.bolong.json")
		test(err, "creating .bolong.json")
		err = json.NewEncoder(f).Encode(&c)
		test(err, "writing .bolong.json")
		err = f.Close()
		test(err, "closing .bolong.json")
	}

	ensureTree := func(tree testTree) {
		t.Helper()
		xremoveAll("testdir/workdir")
		xmkdirAll("testdir/workdir")
		writeConfig()
		for _, d := range tree.dirs {
			xpath := "testdir/workdir/" + d.path
			err := os.MkdirAll(xpath, 0777)
			test(err, "making tree dir")
		}
		for _, f := range tree.files {
			xpath := "testdir/workdir/" + f.path
			fp, err := os.Create(xpath)
			test(err, "creating tree file")
			_, err = fmt.Fprint(fp, f.contents)
			test(err, "write content to tree file")
		}
	}

	indexTree := func(name string) (tt testTree) {
		t.Helper()
		b, err := findBackup(name)
		test(err, "finding latest backup")
		idx, err := readIndex(b)
		test(err, "reading index")
		for _, f := range idx.contents {
			if f.isDir {
				td := testDir{path: f.name}
				tt.dirs = append(tt.dirs, td)
			} else {
				tf := testFile{path: f.name, contents: ""} // no contents yet
				tt.files = append(tt.files, tf)
			}
		}
		return
	}

	compareTree := func(tree, ntree testTree, checkContents bool) {
		t.Helper()
		sortTree := func(t testTree) {
			sort.Slice(t.dirs, func(i, j int) bool {
				return t.dirs[i].path < t.dirs[j].path
			})
			sort.Slice(t.files, func(i, j int) bool {
				return t.files[i].path < t.files[j].path
			})
		}
		sortTree(tree)
		sortTree(ntree)
		if len(tree.dirs) != len(ntree.dirs) {
			t.Errorf("dirs mismatch, %v != %v", tree.dirs, ntree.dirs)
			return
		}
		if len(tree.files) != len(ntree.files) {
			t.Errorf("files mismatch, %v != %v", tree.files, ntree.files)
			return
		}
		for i := range tree.dirs {
			if tree.dirs[i].path != ntree.dirs[i].path {
				t.Errorf("dirs name mismatch, %v != %v", tree.dirs[i].path, ntree.dirs[i].path)
				return
			}
		}
		for i := range tree.files {
			if tree.files[i].path != ntree.files[i].path {
				t.Errorf("files name mismatch, %v != %v", tree.files[i].path, ntree.files[i].path)
			}
			c1 := tree.files[i].contents
			c2 := ntree.files[i].contents
			if checkContents && c1 != c2 {
				t.Errorf("files content mismatch for %s: %q != %q", tree.files[i].path, c1, c2)
			}
		}
	}

	resetRestoreDir := func() {
		t.Helper()
		xremoveAll("testdir/restore")
		xmkdirAll("testdir/restore")
	}

	fsTree := func(dir string) (tree testTree) {
		t.Helper()
		if !strings.HasSuffix(dir, "/") {
			panic("fsTree dir must end with slash")
		}
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !strings.HasPrefix(path, dir) {
				t.Error("bad path prefix")
			}
			relpath := path[len(dir):]
			if relpath == "" {
				relpath = "."
			}
			if relpath == ".bolong.json" || strings.HasSuffix(relpath, "/.bolong.json") {
				return nil
			}
			if info.IsDir() {
				tree.dirs = append(tree.dirs, testDir{path: relpath})
			} else {
				buf, err := ioutil.ReadFile(dir + relpath)
				test(err, "read fs file")
				tree.files = append(tree.files, testFile{path: relpath, contents: string(buf)})
			}
			return nil
		})
		test(err, "fsTree")
		return
	}

	treeInExclude := func(t testTree) (r testTree) {
		r.dirs = t.dirs
		for _, f := range t.files {
			switch f.path {
			case "a/a/excluded.txt", "a/a/not-included.ext":
			default:
				r.files = append(r.files, f)
			}
		}
		return
	}

	xremoveAll("testdir")
	xmkdirAll("testdir/backup")
	xmkdirAll("testdir/workdir")

	writeConfig()

	*configPath = "testdir/workdir/.bolong.json"
	parseConfig()

	// start with simple test
	list([]string{})
	l, err := listBackups()
	test(err, "listing backups")
	if len(l) != 0 {
		t.Error("expected zero backups, found at least one")
	}

	// create new backup
	// check that it is a full, and that all the right files are included/excluded.  just parse the index and see if it is all correct.
	tree1 := testTree{
		files: []testFile{
			{"a/a/excluded.txt", "not in backup"},
			{"a/a/not-included.ext", "not in backup"},
			{"a/a/test.txt", "more"},
			{"a/b/t1.txt", "this is a test"},
			{"a/b/t2.txt", "another test"},
			{"a/b/whitelisted", "included because of a/b/"},
		},
		dirs: []testDir{
			{"."},
			{"a"},
			{"a/a"},
			{"a/b"},
			{"a/c"},
		},
	}
	expTree1 := treeInExclude(tree1)
	// add/remove some files/dirs, and change contents
	tree2 := testTree{
		files: []testFile{
			{"a/a/excluded.txt", "not in backup"},
			{"a/a/not-included.ext", "not in backup"},
			{"a/a/test.txt", "more"},
			// a/b/t1.txt removed, in the middle of tree1's data file
			{"a/b/t2.txt", "different content"}, // updated contents
			{"a/b/t3.txt", "test3"},             // new file
			{"a/b/t4.txt", "test4"},             // new file
			{"a/b/whitelisted", "included because of a/b/"},
		},
		dirs: []testDir{
			{"."},
			{"a"},
			{"a/a"},
			{"a/b"},
			// a/c removed
			{"a/d"}, // a/d added
		},
	}
	expTree2 := treeInExclude(tree2)

	// change all the files from tree2, so it is no longer needed when restoring
	tree3 := testTree{
		files: []testFile{
			{"a/a/excluded.txt", "not in backup"},
			{"a/a/not-included.ext", "not in backup"},
			{"a/a/test.txt", "more"},
			{"a/b/t2.txt", "new different content"},
			{"a/b/t3.txt", "new test3"},
			{"a/b/t4.txt", "new test4"},
			{"a/b/whitelisted", "included because of a/b/"},
		},
		dirs: []testDir{
			{"."},
			{"a"},
			{"a/a"},
			{"a/b"},
			{"a/d"},
		},
	}
	expTree3 := treeInExclude(tree3)

	ensureTree(tree1)
	compareTree(tree1, fsTree("testdir/workdir/"), true)
	backupCmd([]string{"testdir/workdir"}, "20171222-0001")

	l, err = listBackups()
	test(err, "listing backups")
	if len(l) != 1 {
		t.Errorf("expected single backup, found %d", len(l))
	}
	ntree := indexTree("latest")
	compareTree(expTree1, ntree, false)

	// do a restore
	// check that it restored all files correctly
	resetRestoreDir()
	restoreCmd([]string{"-quiet", "testdir/restore"})
	compareTree(expTree1, fsTree("testdir/restore/"), true)

	ensureTree(tree2)
	backupCmd([]string{"testdir/workdir"}, "20171222-0002")
	resetRestoreDir()
	restoreCmd([]string{"-quiet", "testdir/restore"})
	compareTree(expTree2, fsTree("testdir/restore/"), true)

	ensureTree(tree3)
	backupCmd([]string{"testdir/workdir"}, "20171222-0003")
	resetRestoreDir()
	restoreCmd([]string{"-quiet", "testdir/restore"})
	compareTree(expTree3, fsTree("testdir/restore/"), true)

	// so far we have 1 fulll, 2 incrementals
	backupCmd([]string{"testdir/workdir"}, "20171222-0004") // full
	backupCmd([]string{"testdir/workdir"}, "20171222-0005") // incr
	backupCmd([]string{"testdir/workdir"}, "20171222-0006") // incr
	backupCmd([]string{"testdir/workdir"}, "20171222-0007") // full
	backupCmd([]string{"testdir/workdir"}, "20171222-0008") // incr
	// we should now have 2 full, 1 incr
	l, err = listBackups()
	test(err, "listing backups")
	if len(l) != 3 {
		t.Errorf("expected to have 4 backups, have %d, %#v", len(l), l)
		return
	}
	expect := []string{
		"20171222-0004",
		"20171222-0007",
		"20171222-0008",
	}
	for i, exp := range expect {
		if l[i].name != exp {
			t.Errorf("expected backup %d to be %s, saw %s", i, exp, l[i].name)
			return
		}
	}

	ensureTree(tree3)
	backupCmd([]string{"testdir/workdir"}, "20171222-009")
	resetRestoreDir()
	restoreCmd([]string{"-quiet", "testdir/restore", "^a/a/", "/whitelisted$"})
	xExpTree3 := testTree{
		files: []testFile{
			{"a/a/test.txt", "more"},
			{"a/b/whitelisted", "included because of a/b/"},
		},
		dirs: []testDir{
			{"."},
			{"a"},
			{"a/a"},
			{"a/b"},
		},
	}
	compareTree(xExpTree3, fsTree("testdir/restore/"), true)
}
