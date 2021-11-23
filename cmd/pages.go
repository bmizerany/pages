package main

import (
	"flag"
	"log"

	"github.com/bmizerany/pages"
)

var (
	flagVerbose = flag.Bool("v", false, "enable verbose logging")
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("pages: ")

	flag.Parse()

	cfg := &pages.Config{}
	if *flagVerbose {
		cfg.Logf = log.Printf
	}

	if err := pages.Run(cfg); err != nil {
		log.Fatal(err)
	}
}
