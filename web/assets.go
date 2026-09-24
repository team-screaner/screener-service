// Package web embeds the Screener browser application.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed dist/*
var files embed.FS

// Handler serves the application entrypoint and its local static assets.
func Handler() http.Handler {
	assets, err := fs.Sub(files, "dist")
	if err != nil {
		panic(err)
	}
	static := http.FileServer(http.FS(assets))
	mux := http.NewServeMux()
	mux.Handle("GET /{$}", static)
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", static))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("Referrer-Policy", "same-origin")
		mux.ServeHTTP(w, r)
	})
}
