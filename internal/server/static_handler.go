package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mirkobrombin/goup/internal/assets"
)

// ServeStatic serves static files with support for pre-compressed sidecar files (.br, .gz).
func ServeStatic(w http.ResponseWriter, r *http.Request, root string) {
	cleanPath := filepath.Clean(r.URL.Path)
	fullPath := filepath.Join(root, cleanPath)

	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			if isBrowser(r) {
				assets.RenderErrorPage(w, http.StatusNotFound, "Page Not Found", "The page you are looking for does not exist.")
			} else {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprintln(w, "404 Not Found")
			}
			return
		}
		if isBrowser(r) {
			assets.RenderErrorPage(w, http.StatusInternalServerError, "Internal Server Error", "Something went wrong on our end.")
		} else {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(w, "500 Internal Server Error")
		}
		return
	}

	if info.IsDir() {
		indexPath := filepath.Join(fullPath, "index.html")
		indexInfo, err := os.Stat(indexPath)
		if err == nil && !indexInfo.IsDir() {
			fullPath = indexPath
			info = indexInfo
		} else {
			// Directory listing or Welcome Page
			if cleanPath == "/" || cleanPath == "." || cleanPath == "\\" {
				// If index.html is missing at root, we can still show listing if it's not empty
			}

			entries, err := os.ReadDir(fullPath)
			if err != nil {
				if isBrowser(r) {
					assets.RenderErrorPage(w, http.StatusInternalServerError, "Internal Server Error", "Unable to read directory.")
				} else {
					w.Header().Set("Content-Type", "text/plain; charset=utf-8")
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprintln(w, "500 Internal Server Error: Unable to read directory.")
				}
				return
			}

			if isBrowser(r) {
				var items []assets.ListingItem
				for _, entry := range entries {
					entryInfo, _ := entry.Info()
					items = append(items, assets.ListingItem{
						Name:    entry.Name(),
						IsDir:   entry.IsDir(),
						Size:    formatSizeBytes(entryInfo.Size()),
						ModTime: entryInfo.ModTime().Format("2006-01-02 15:04:05"),
					})
				}

				showBack := cleanPath != "/" && cleanPath != "." && cleanPath != "\\"
				assets.RenderDirectoryListing(w, cleanPath, items, showBack)
			} else {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				for _, entry := range entries {
					name := entry.Name()
					if entry.IsDir() {
						name += "/"
					}
					fmt.Fprintln(w, name)
				}
			}
			return
		}
	}

	acceptEncoding := r.Header.Get("Accept-Encoding")
	servedCompressed := false
	var servePath string
	var serveInfo os.FileInfo
	var contentEncoding string

	if strings.Contains(acceptEncoding, "br") {
		brPath := fullPath + ".br"
		if brInfo, err := os.Stat(brPath); err == nil && !brInfo.IsDir() {
			servePath = brPath
			serveInfo = brInfo
			contentEncoding = "br"
			servedCompressed = true
		}
	}

	if !servedCompressed && strings.Contains(acceptEncoding, "gzip") {
		gzPath := fullPath + ".gz"
		if gzInfo, err := os.Stat(gzPath); err == nil && !gzInfo.IsDir() {
			servePath = gzPath
			serveInfo = gzInfo
			contentEncoding = "gzip"
			servedCompressed = true
		}
	}

	if !servedCompressed {
		servePath = fullPath
		serveInfo = info
	}

	file, err := os.Open(servePath)
	if err != nil {
		if isBrowser(r) {
			assets.RenderErrorPage(w, http.StatusInternalServerError, "Internal Server Error", "Unable to read file content.")
		} else {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(w, "500 Internal Server Error: Unable to read file content.")
		}
		return
	}
	defer file.Close()

	w.Header().Add("Vary", "Accept-Encoding")

	if servedCompressed {
		w.Header().Set("Content-Encoding", contentEncoding)
		mimeType := mime.TypeByExtension(filepath.Ext(fullPath))
		if mimeType == "" {
			// Sniffing won't work on compressed data, so default if unknown
			mimeType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", mimeType)
	}

	etag := fmt.Sprintf("\"%x-%x\"", serveInfo.Size(), serveInfo.ModTime().UnixNano())
	w.Header().Set("ETag", etag)

	http.ServeContent(w, r, filepath.Base(fullPath), serveInfo.ModTime(), file)
}

// Custom ETag calculation (unused in simplified version, but kept for reference)
func calculateETag(info os.FileInfo) string {
	hash := sha256.New()
	hash.Write([]byte(strconv.FormatInt(info.Size(), 10)))
	hash.Write([]byte(strconv.FormatInt(info.ModTime().UnixNano(), 10)))
	return hex.EncodeToString(hash.Sum(nil))
}
func formatSizeBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
func isBrowser(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "text/html")
}
