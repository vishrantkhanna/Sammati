// Package web holds the pages and scripts built into the program.
package web

import (
	"embed"
	"mime"
	"net/http"
	"path"
)

// Version is the release shown in the admin footer.
const Version = "0.1.0"

//go:embed static
var files embed.FS

// Serve writes one built-in file, or 404.
func Serve(w http.ResponseWriter, name string) {
	body, err := files.ReadFile("static/" + path.Base(name))
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(body)
}
