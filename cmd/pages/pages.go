package main

import (
	"flag"
	"log"
	"os"

	"blake.io/pages"
)

var (
	flagVerbose      = flag.Bool("v", false, "enable verbose logging")
	flagRemovePublic = flag.Bool("rm", false, "forcefully remove ./public")
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("pages: ")

	flag.Parse()

	cfg := &pages.Config{}
	if *flagVerbose {
		cfg.Logf = log.Printf
	}

	if *flagRemovePublic {
		os.RemoveAll("./public")
	}

	fsys := os.DirFS(".")
	if err := pages.Run(fsys, cfg); err != nil {
		log.Fatal(err)
	}
}
