package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"path"
	"strings"
)

var (
	configPath = flag.String("config", "", "path to config file")
	remotePath = flag.String("path", "", "path at remote storage, overrides config file")
	config     struct {
		Kind  string
		Local struct {
			Path string
		}
		GoogleS3 struct {
			AccessKey,
			Secret,
			Bucket,
			Path string
		}
		Include                []string
		Exclude                []string
		IncrementalsPerFull    int
		FullKeep               int
		IncrementalForFullKeep int
		Passphrase             string
	}
	remote Remote
)

func check(err error, msg string) {
	if err == nil {
		return
	}
	if msg == "" {
		log.Fatal(err)
	}
	log.Fatalf("%s: %s\n", msg, err)
}

func main() {
	log.SetFlags(0)
	flag.Usage = func() {
		log.Println("usage:")
		log.Println("\tbackup [-config config.json] [-path path] backup [flags] [directory]")
		log.Println("\tbackup [-config config.json] [-path path] restore [flags] destination [backup-id]")
		log.Println("\tbackup [-config config.json] [-path path] list")
	}
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		os.Exit(1)
	}

	if *configPath == "" {
		findConfigPath()
	}
	f, err := os.Open(*configPath)
	check(err, "opening config file")
	// xxx should parse strictly, throwing error for unrecognized fields..
	err = json.NewDecoder(f).Decode(&config)
	check(err, "parsing config file")

	switch config.Kind {
	case "local":
		if *remotePath != "" {
			config.Local.Path = *remotePath
		}
		if config.Local.Path == "" {
			log.Fatal(`field "local" must be set for kind "local"`)
		}
		path := config.Local.Path
		if !strings.HasSuffix(path, "/") {
			path += "/"
		}
		remote = &Local{path}
	case "googles3":
		if *remotePath != "" {
			config.GoogleS3.Path = *remotePath
		}
		if config.GoogleS3.AccessKey == "" || config.GoogleS3.Secret == "" || config.GoogleS3.Bucket == "" || config.GoogleS3.Path == "" {
			log.Fatal(`fields "googles3.accessKey", "googles3.secret", googles3.bucket" and  "googles3.path" must be set`)
		}
		path := config.GoogleS3.Path
		if !strings.HasPrefix(path, "/") || !strings.HasSuffix(path, "/") {
			log.Fatal(`field "googles3.path" must start and end with a slash`)
		}
		remote = &GoogleS3{config.GoogleS3.Bucket, path}
	case "":
		log.Fatal(`missing field "kind", must be "local" or "googles3"`)
	default:
		log.Fatalf(`unknown remote kind "%s"`, config.Kind)
	}

	cmd := args[0]
	args = args[1:]
	switch cmd {
	case "backup":
		backup(args)
	case "restore":
		restore(args)
	case "list":
		list(args)
	default:
		flag.Usage()
		os.Exit(1)
	}
}

func findConfigPath() {
	dir, err := os.Getwd()
	check(err, "looking for config file in current working directory")
	for {
		xpath := dir + "/.bolong.json"
		_, err := os.Stat(xpath)
		if err == nil {
			*configPath = xpath
			return
		}
		if !os.IsNotExist(err) {
			log.Fatal("cannot find a .bolong.json up in directory hierarchy")
		}
		ndir := path.Dir(dir)
		if ndir == dir {
			log.Fatal("cannot find a .bolong.json up in directory hierarchy")
		}
		dir = ndir
	}
}
