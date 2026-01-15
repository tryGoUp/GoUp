package dashboard

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/mirkobrombin/goup/internal/config"
)

//go:embed static/*
var content embed.FS

// apiProxy returns a reverse proxy handler to forward /api calls to the backend.
func apiProxy() http.Handler {
	target, err := url.Parse(fmt.Sprintf("http://localhost:%d", config.GlobalConf.APIPort))
	if err != nil {
		panic(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		if config.GlobalConf != nil && config.GlobalConf.Account.APIToken != "" {
			req.Header.Set("X-API-Token", config.GlobalConf.Account.APIToken)
		}
	}
	return proxy
}

// spaFileServer returns an HTTP handler that serves static files from the
// given FS. If the requested file does not exist, it falls back to serving
// index.html (SPA fallback).
func spaFileServer(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqPath := strings.TrimPrefix(r.URL.Path, "/")
		if reqPath == "" {
			reqPath = "index.html"
		}
		if _, err := fs.Stat(fsys, reqPath); err != nil {
			r.URL.Path = "/index.html"
		}
		fileServer.ServeHTTP(w, r)
	})
}

// Handler returns the HTTP handler for the dashboard.
func Handler() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("/api/", apiProxy())

	subFS, err := fs.Sub(content, "static")
	if err != nil {
		panic(err)
	}

	mux.Handle("/", spaFileServer(subFS))
	return mux
}
