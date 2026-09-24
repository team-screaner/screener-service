package http

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"time"
)

//go:embed swagger/*
var swaggerFiles embed.FS

func documentationHandler(contract []byte) http.Handler {
	assets, err := fs.Sub(swaggerFiles, "swagger")
	if err != nil {
		panic(err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /docs/", http.StripPrefix("/docs/", http.FileServer(http.FS(assets))))
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		http.ServeContent(w, r, "openapi.json", time.Time{}, bytes.NewReader(contract))
	})
	return mux
}
