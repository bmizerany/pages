package live

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/dietsche/rfsnotify"
	"golang.org/x/net/html"
)

const injectHTML = `<iframe src="/_reloader" style="display: none"></iframe>`

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

// WriteReloadableError is a convenience helper that writes err in a pre tag
// within body tag to w so that the response is picked up by Reloader.
//
// Users that want to customize the error page can build their own HTML, but
// should be sure a body tag is present.
func WriteReloadableError(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(code)
	fmt.Fprintf(w, "<body><pre>%s</pre></body>", err)
}

func Reloader(watchPath string, stderr io.Writer, inner http.Handler) (http.Handler, error) {
	// TODO: change stderr to a logger

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_updates":
			// TODO: Track update counts and send down with reloaderHTML for it to
			// round-trip back so that we can determine if another reload is needed
			// right away. This also allow for a single watcher, instead of one per
			// request.
			//
			// Currently we can miss events that happened after the
			// page making this request was being served and loaded.
			// In practice, this is fine because it's somewhat rare
			// (depending), but could be a little annoying for users
			// when it does happen. It can be avoided by using the
			// counter mentioned above along with a single watcher
			// waking up these update goroutines.
			watch, err := rfsnotify.NewWatcher()
			if err != nil {
				// ask user to manually reload to avoid infinite loop browser -> server -> browser ...
				http.Error(w, "Error starting watcher. Please fix and reload.", 500)
				fmt.Fprintf(stderr, "pages: error creating the watcher %s: %v", watchPath, err)
				return
			}
			defer watch.Close()
			if err := watch.AddRecursive(watchPath); err != nil {
				WriteReloadableError(w, 500, err)
				http.Error(w, "Error watching. Please fix and reload.", 500)
				fmt.Fprintf(stderr, "pages: error watching %s: %v", watchPath, err)
				return
			}

			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			maybeFlush(w)

			for {
				select {
				case <-watch.Events:
					// TODO(bmizerany): check if error event
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

	var injected bool
	z := html.NewTokenizer(r.buf)
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if err := z.Err(); err == io.EOF {
				break
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
				injected = true
			}
		}
		_, err := r.w.Write(z.Raw())
		if err != nil {
			return err
		}
	}
	if !injected {
		_, err := io.WriteString(r.w, injectHTML)
		if err != nil {
			return err
		}
	}
	return nil
}

func maybeFlush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func isHTML(r http.Header) bool {
	return strings.HasPrefix(r.Get("Content-Type"), "text/html")
}
