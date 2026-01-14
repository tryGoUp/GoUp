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
			assets.RenderErrorPage(w, http.StatusNotFound, "Page Not Found", "The page you are looking for does not exist.")
			return
		}
		assets.RenderErrorPage(w, http.StatusInternalServerError, "Internal Server Error", "Something went wrong on our end.")
		return
	}

	if info.IsDir() {
		indexPath := filepath.Join(fullPath, "index.html")
		indexInfo, err := os.Stat(indexPath)
		if err == nil && !indexInfo.IsDir() {
			fullPath = indexPath
			info = indexInfo
		} else {
			// Check if we are at root and index is missing -> Welcome Page
			if cleanPath == "/" || cleanPath == "." || cleanPath == "\\" {
				assets.RenderWelcomePage(w)
				return
			}
			assets.RenderErrorPage(w, http.StatusNotFound, "Page Not Found", "The page you are looking for does not exist.")
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
		assets.RenderErrorPage(w, http.StatusInternalServerError, "Internal Server Error", "Unable to read file content.")
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
