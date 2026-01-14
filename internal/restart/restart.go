package restart

import (
	"context"
	"log"
	"net/http"
	"os"
	"syscall"
	"time"
)

var srv *http.Server

// SetServer sets the server instance to be restarted.
func SetServer(s *http.Server) {
	srv = s
}

// RestartHandler handles the HTTP request to restart the server.
func RestartHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Server is restarting..."))
	go func() {
		time.Sleep(100 * time.Millisecond) // allow response to be sent
		Restart()
	}()
}

// Restart gracefully shuts down the server and re-executes the process.
func Restart() {
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("Error during shutdown: %v", err)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get executable: %v", err)
	}
	args := os.Args
	env := os.Environ()
	// Re-exec the process.
	if err := syscall.Exec(exe, args, env); err != nil {
		log.Fatalf("Failed to exec: %v", err)
	}
}

// RestartServer is an alias for Restart (legacy support / clarity).
func RestartServer() {
	Restart()
}

// ForceRestart restarts the server immediately without waiting for graceful shutdown.
func ForceRestart() {
	exe, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get executable: %v", err)
	}
	args := os.Args
	env := os.Environ()
	// Re-exec the process immediately (process replacement)
	if err := syscall.Exec(exe, args, env); err != nil {
		log.Fatalf("Failed to exec: %v", err)
	}
}

// ScheduleRestart schedules a restart in `seconds` seconds.
func ScheduleRestart(seconds int) {
	go func() {
		time.Sleep(time.Duration(seconds) * time.Second)
		Restart()
	}()
}
