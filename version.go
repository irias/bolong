package main

import (
	"flag"
	"fmt"
	"log"
	"os"
)

func _version(args []string) {
	fs := flag.NewFlagSet("version", flag.ExitOnError)
	fs.Usage = func() {
		log.Println("usage: bolong [flags] version")
		fs.PrintDefaults()
	}
	err := fs.Parse(args)
	if err != nil {
		log.Println(err)
		fs.Usage()
		os.Exit(2)
	}
	args = fs.Args()
	if len(args) != 0 {
		fs.Usage()
		os.Exit(2)
	}

	fmt.Println(version)
}
