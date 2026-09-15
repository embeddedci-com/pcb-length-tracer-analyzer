package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// spaHandler serves a built single-page app, falling back to index.html for
// paths the bundle routes client-side.
//
// Without the fallback, reloading the page on /tools/pcb-trace-length-analyzer/<session id>
// asks the server for a file that does not exist and gets a 404 -- the app
// works until the user presses refresh, which is the sort of thing that is
// noticed late. Anything under /assets/ is served strictly, because a missing
// asset must report itself as missing rather than quietly returning HTML that
// the browser then fails to parse as JavaScript.
func spaHandler(dir string) (http.Handler, error) {
	if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
		return nil, err
	}
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if clean == "." {
			clean = "index.html"
		}
		// Never let a path escape the bundle directory.
		if strings.HasPrefix(clean, "..") {
			http.NotFound(w, r)
			return
		}
		if _, err := os.Stat(filepath.Join(dir, clean)); err == nil {
			files.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") || strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	}), nil
}
