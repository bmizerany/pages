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
		if r.URL.Path == "/_updates" {
			select {
			case <-watch.Events:
			case <-r.Context().Done():
				return
			}

			io.WriteString(w, `
				<!DOCTYPE html>
				<html>
				<head>
				<script>
					window.top.location.reload();
				</script>
				</head>
				</html>
			`)
			return
		} else {
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

const iframeHTML = `<iframe src="/_updates" style="display: none"></iframe>`

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
				_, err := io.WriteString(r.w, iframeHTML)
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
