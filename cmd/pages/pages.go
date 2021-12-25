package main

import (
	"flag"
	"log"
	"os"
	"plugin"

	"blake.io/pages"
)

var (
	flagVerbose      = flag.Bool("v", false, "enable verbose logging")
	flagRemovePublic = flag.Bool("rm", false, "forcefully remove ./public")
	flagPlugin       = flag.String("p", "", "load funcs and data from Go plugin")
)

// TODO(bmizerany): load JSON data from pages.json if -p not set

func main() {
	log.SetFlags(0)
	log.SetPrefix("pages: ")

	flag.Parse()

	cfg := &pages.Config{}
	if *flagVerbose {
		cfg.Logf = log.Printf
	}

	if *flagPlugin != "" {
		p, err := plugin.Open(*flagPlugin)
		if err != nil {
			log.Fatal(err)
		}
		funcs, err := p.Lookup("Funcs")
		if err != nil {
			log.Fatal(err)
		}
		data, err := p.Lookup("Data")
		if err != nil {
			log.Fatal(err)
		}
		cfg.Funcs = *funcs.(*map[string]interface{})
		cfg.Data = data
	}

	if *flagRemovePublic {
		os.RemoveAll("./public")
	}

	fsys := os.DirFS(".")
	if err := pages.Run(fsys, cfg); err != nil {
		log.Fatal(err)
	}
}
