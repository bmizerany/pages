package live

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/net/html"
)

const injectHTML = `
<iframe src="/_reloader" style="display: none"></iframe>
`

const reloaderHTML = `
<!DOCTYPE html>
<html>
<head>
<script>
var evs = new EventSource('/_updates');
evs.onmessage = function() { window.top.location.reload(); };
evs.onerror = function(e) { console.debug('pages: error', e); };
</script>
</head>
</html>
`

func Reloader(watchPath string, stderr io.Writer, inner http.Handler) (http.Handler, error) {
	watch, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if err := watch.Add(watchPath); err != nil {
		return nil, err
	}

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stderr := io.MultiWriter(w, stderr)
		switch r.URL.Path {
		case "/_updates":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			maybeFlush(w)

			for {
				// TODO: heartbeat?
				select {
				case <-watch.Events:
					io.WriteString(w, "data: {}\n\n\n")
					maybeFlush(w)
				case <-r.Context().Done():
					return
				}
			}
		case "/_reloader":
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, reloaderHTML)
		default:
			ri := &reloadInjector{w: w}
			inner.ServeHTTP(ri, r)
			if err := ri.Finish(); err != nil {
				fmt.Fprintf(stderr, "pages: %v", err)
				return
			}
		}
	})

	return h, nil
}

type reloadInjector struct {
	buf  *bytes.Buffer
	code int
	w    http.ResponseWriter
}

func (r *reloadInjector) Write(p []byte) (int, error) {
	if r.buf == nil {
		r.buf = &bytes.Buffer{}
	}
	return r.buf.Write(p)
}

func (r *reloadInjector) WriteHeader(code int) { r.code = code }
func (r *reloadInjector) Header() http.Header  { return r.w.Header() }

func (r *reloadInjector) Finish() error {
	if r.buf == nil {
		return nil
	}

	if !isHTML(r.w.Header()) {
		_, err := r.w.Write(r.buf.Bytes())
		return err
	}

	// any content-length header is now invalid; remove and use chunked
	r.w.Header().Del("Content-Length")
	if r.code != 0 {
		r.w.WriteHeader(r.code)
	}

	z := html.NewTokenizer(r.buf)
	for {
		tt := z.Next()
		fmt.Printf("token: %v %s\n", tt, z.Raw())
		if tt == html.ErrorToken {
			if err := z.Err(); err == io.EOF {
				return nil
			}
			return z.Err()
		}
		if tt == html.EndTagToken {
			t := z.Token()
			if t.Data == "body" {
				_, err := io.WriteString(r.w, injectHTML)
				if err != nil {
					return err
				}
			}
		}
		_, err := r.w.Write(z.Raw())
		if err != nil {
			return err
		}
	}
}

func maybeFlush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func isHTML(r http.Header) bool {
	return strings.HasPrefix(r.Get("Content-Type"), "text/html")
}
