package main

import (
	"flag"
	"fmt"
	"log"
	"os"
)

func listfiles(args []string) {
	fs := flag.NewFlagSet("listfiles", flag.ExitOnError)
	name := fs.String("name", "latest", "name of backup to list files for")
	verbose := fs.Bool("verbose", false, "verbose printing, including permissions and size")
	fs.Usage = func() {
		log.Println("usage: bolong [flags] listfiles [flags]")
		fs.PrintDefaults()
	}
	fs.Parse(args)
	args = fs.Args()
	if len(args) != 0 {
		fs.Usage()
		os.Exit(2)
	}

	backups, err := findBackupChain(*name)
	check(err, "finding backup")
	idx, err := readIndex(backups[0])
	check(err, "parsing index")
	for _, f := range idx.contents {
		name := f.name
		if f.isDir {
			name += "/"
		}
		if *verbose {
			var kind, size, user, group string
			if f.isDir {
				size = fmt.Sprintf("%10s", "")
				kind = "d"
			} else {
				kind = "f"
				if f.isSymlink {
					kind = "s"
				}
				size = fmt.Sprintf("%10d", f.size)
			}
			user = fmt.Sprintf("%10s", f.user)
			group = fmt.Sprintf("%10s", f.group)
			fmt.Printf("%s %04o %s %s %s %s\n", kind, f.permissions, size, user, group, name)
		} else {
			fmt.Println(name)
		}
	}
}
