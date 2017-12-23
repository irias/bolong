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
		log.Println("usage: bolong [flags] listflags [flags]")
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
			var size string
			if f.isDir {
				size = fmt.Sprintf("%10s", "")
			} else {
				size = fmt.Sprintf("%10d", f.size)
			}
			fmt.Printf("%04o %s %s\n", f.permissions, size, name)
		} else {
			fmt.Println(name)
		}
	}
}
