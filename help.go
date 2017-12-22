package main

import (
	"flag"
	"log"
	"os"
)

func help(args []string) {
	fs := flag.NewFlagSet("help", flag.ExitOnError)
	fs.Usage = func() {
		log.Println("usage: bolong [flags] help")
		fs.PrintDefaults()
	}
	fs.Parse(args)
	args = fs.Args()
	if len(args) != 0 {
		fs.Usage()
		os.Exit(2)
	}

	flag.Usage() // usage from main command
	printExampleConfig()
	log.Println("")
	log.Println("See https://github.com/irias/bolong for details.")
}
