package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func staticFileHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/itemchecklist")
	if path == "/" || path == "" {
		path = "/index.html"
	}

	filePath := filepath.Join("public", path)
	data, err := os.ReadFile(filePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	contentType := "text/plain"
	switch filepath.Ext(filePath) {
	case ".html":
		contentType = "text/html; charset=utf-8"
	case ".css":
		contentType = "text/css; charset=utf-8"
	case ".js":
		contentType = "application/javascript; charset=utf-8"
	case ".json":
		contentType = "application/json; charset=utf-8"
	case ".png":
		contentType = "image/png"
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".gif":
		contentType = "image/gif"
	case ".svg":
		contentType = "image/svg+xml"
	case ".ico":
		contentType = "image/x-icon"
	case ".woff":
		contentType = "font/woff"
	case ".woff2":
		contentType = "font/woff2"
	case ".ttf":
		contentType = "font/ttf"
	case ".eot":
		contentType = "application/vnd.ms-fontobject"
	}

	w.Header().Set("Content-Type", contentType)
	w.Write(data)
}
