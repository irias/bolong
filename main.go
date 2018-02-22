package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"path"
	"strings"
	"time"
)

type configuration struct {
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

var (
	version    = "dev"
	configPath = flag.String("config", "", "path to config file")
	remotePath = flag.String("path", "", "path at remote storage, overrides config file")
	config     configuration
	store      destination
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
		log.Println("bolong [flags] backup [flags] [directory]")
		log.Println("bolong [flags] restore [flags] destination [path-regexp ...]")
		log.Println("bolong [flags] list")
		log.Println("bolong [flags] listfiles [flags]")
		log.Println("bolong [flags] dumpindex [name]")
		log.Println("bolong [flags] version")
		log.Println("bolong [flags] help")
		flag.PrintDefaults()
	}
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		os.Exit(1)
	}

	cmd := args[0]
	args = args[1:]
	switch cmd {
	case "backup":
		parseConfig()
		// create name from timestamp now, for simpler testcode
		name := time.Now().UTC().Format("20060102-150405")
		backupCmd(args, name)
	case "restore":
		parseConfig()
		restoreCmd(args)
	case "list":
		parseConfig()
		list(args)
	case "listfiles":
		parseConfig()
		listfiles(args)
	case "dumpindex":
		parseConfig()
		dumpindex(args)
	case "version":
		_version(args)
	case "help":
		help(args)
	default:
		flag.Usage()
		os.Exit(1)
	}
}

func parseConfig() {
	if *configPath == "" {
		findConfigPath()
	}
	f, err := os.Open(*configPath)
	check(err, "opening config file")
	// xxx should parse strictly, throwing error for unrecognized fields..
	err = json.NewDecoder(f).Decode(&config)
	check(err, "parsing config file")

	switch config.Kind {
	default:
		log.Fatalf(`unknown remote kind "%s"`, config.Kind)
	case "":
		log.Print(`missing field "kind", must be "local" or "googles3"`)
		printExampleConfig()
		os.Exit(2)
	case "local":
		if *remotePath != "" {
			config.Local.Path = *remotePath
		}
		if config.Local.Path == "" {
			log.Print(`field "local" must be set for kind "local"`)
			printExampleConfig()
			os.Exit(2)
		}
		path := config.Local.Path
		if !strings.HasSuffix(path, "/") {
			path += "/"
		}
		store = &local{path}
	case "googles3":
		if *remotePath != "" {
			config.GoogleS3.Path = *remotePath
		}
		if config.GoogleS3.AccessKey == "" || config.GoogleS3.Secret == "" || config.GoogleS3.Bucket == "" || config.GoogleS3.Path == "" {
			log.Print(`fields "googles3.accessKey", "googles3.secret", googles3.bucket" and  "googles3.path" must be set`)
			printExampleConfig()
			os.Exit(2)
		}
		path := config.GoogleS3.Path
		if !strings.HasPrefix(path, "/") || !strings.HasSuffix(path, "/") {
			log.Fatal(`field "googles3.path" must start and end with a slash`)
		}
		store = &googleS3{config.GoogleS3.Bucket, path}
	}
	if config.Passphrase == "" {
		log.Fatalln("passphrase cannot be empty")
	}
}

func printExampleConfig() {
	log.Print(`
example config file:
{
	"kind": "googles3",
	"googles3": {
		"accessKey": "GOOGLTEST123456789",
		"secret": "bm90IGEgcmVhbCBrZXkuIG5pY2UgdHJ5IHRob3VnaCBeXg==",
		"bucket": "your-bucket-name",
		"path": "/projectname/"
	},
	"passphrase": "your secret passphrase"
}
`)
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
			log.Print("cannot find a .bolong.json up in directory hierarchy")
			printExampleConfig()
			os.Exit(2)
		}
		ndir := path.Dir(dir)
		if ndir == dir {
			log.Print("cannot find a .bolong.json up in directory hierarchy")
			printExampleConfig()
			os.Exit(2)
		}
		dir = ndir
	}
}
