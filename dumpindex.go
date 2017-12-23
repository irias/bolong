package main

import (
	"flag"
	"log"
	"os"
)

func dumpindex(args []string) {
	fs := flag.NewFlagSet("dumpindex", flag.ExitOnError)
	fs.Usage = func() {
		log.Println("usage: bolong [flags] dumpindex [name]")
		fs.PrintDefaults()
	}
	fs.Parse(args)
	args = fs.Args()
	if len(args) > 1 {
		fs.Usage()
		os.Exit(2)
	}

	name := "latest"
	if len(args) == 1 {
		name = args[0]
	}
	backup, err := findBackup(name)
	check(err, "looking up backup")
	idx, err := readIndex(backup)
	check(err, "reading index")
	err = writeIndex(os.Stdout, idx)
	check(err, "writing index")
}
