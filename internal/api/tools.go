package api

import (
	"archive/zip"
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gorilla/mux"
	"github.com/mirkobrombin/goup/internal/config"
)

func cleanupLogsHandler(w http.ResponseWriter, r *http.Request) {
	logDir := config.GetLogDir()
	zipBuffer := new(bytes.Buffer)
	zipWriter := zip.NewWriter(zipBuffer)

	err := filepath.Walk(logDir, func(file string, info fs.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		relPath, err := filepath.Rel(logDir, file)
		if err != nil {
			return err
		}
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		defer f.Close()
		fw, err := zipWriter.Create(relPath)
		if err != nil {
			return err
		}
		_, err = io.Copy(fw, f)
		return err
	})
	if err != nil {
		http.Error(w, "Error zipping logs", http.StatusInternalServerError)
		return
	}
	zipWriter.Close()

	backupFile := filepath.Join(logDir, "logs_backup_"+time.Now().Format("20060102_150405")+".zip")
	err = os.WriteFile(backupFile, zipBuffer.Bytes(), 0600)
	if err != nil {
		http.Error(w, "Error saving backup zip", http.StatusInternalServerError)
		return
	}

	err = filepath.Walk(logDir, func(file string, info fs.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if file == backupFile {
			return nil
		}
		return os.Remove(file)
	})
	if err != nil {
		http.Error(w, "Error cleaning logs", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]string{
		"message":    "Logs cleaned and backup saved",
		"backupFile": backupFile,
	})
}

func SetupToolsRoutes(r *mux.Router) {
	r.HandleFunc("/api/tools/cleanuplogs", cleanupLogsHandler).Methods("POST")
}
