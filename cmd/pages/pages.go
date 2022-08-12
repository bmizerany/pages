package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"plugin"

	"blake.io/pages"
)

var (
	flagVerbose      = flag.Bool("v", false, "enable verbose logging")
	flagRemovePublic = flag.Bool("rm", false, "forcefully remove ./public")
	flagPlugin       = flag.String("p", "", "load funcs and data from Go plugin")
	flagHTTP         = flag.String("http", "", "HTTP service address (default \"localhost:6060\")")
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
		pname := *flagPlugin

		p, err := plugin.Open(pname)
		if err != nil {
			log.Fatal(err)
		}
		funcs, err := p.Lookup("Funcs")
		if err != nil {
			log.Printf("%s: Funcs symbol not found; skipping", pname)
		} else {
			fm, ok := funcs.(*map[string]any)
			if !ok {
				log.Fatalf("%s: Funcs must be map[string]any; got %T", pname, funcs)
			}
			cfg.Funcs = *fm
		}
		data, err := p.Lookup("Data")
		if err != nil {
			log.Printf("%s: Data symbol not found; skipping", pname)
		} else {
			cfg.Data = data
		}
	}

	// ensure we're in a pages project before possibly removing ./public
	// (which may be unintended by the user)
	_, err := os.Stat("./pages")
	if err != nil {
		if os.IsNotExist(err) {
			log.Fatal("pages directory not found; please create one and try again.")
		}
		log.Fatal(err)
	}

	if *flagRemovePublic {
		os.RemoveAll("./public")
	}

	fsys := os.DirFS(".")
	if *flagHTTP != "" {
		log.Fatal(serveLiveReload(*flagHTTP, fsys, cfg))
	} else {
		if err := pages.Run(fsys, cfg); err != nil {
			log.Fatal(err)
		}
	}

}

func serveLiveReload(addr string, fsys fs.FS, cfg *pages.Config) error {
	if addr == "" {
		addr = "localhost:6060"
	}

	hfs := http.FileServer(http.FS(fsys))
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		os.RemoveAll("./public")

		if err := pages.Run(fsys, cfg); err != nil {
			fmt.Fprintf(io.MultiWriter(w, os.Stderr), "pages: %v", err)
			return
		}

		hfs.ServeHTTP(w, r)
	})

	return http.ListenAndServe(addr, h)
}
