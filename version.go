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
	fs.Parse(args)
	args = fs.Args()
	if len(args) != 0 {
		fs.Usage()
		os.Exit(2)
	}

	fmt.Println(version)
}
