package plugins

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mirkobrombin/goup/internal/config"
	"github.com/mirkobrombin/goup/internal/logger"
)

// findPHPFPM returns the path to a php-fpm binary, or "" when unavailable.
func findPHPFPM() string {
	if p, err := exec.LookPath("php-fpm"); err == nil {
		return p
	}
	matches, _ := filepath.Glob("/usr/sbin/php-fpm*")
	if len(matches) > 0 {
		return matches[len(matches)-1]
	}
	return ""
}

func TestPHPPlugin_EndToEnd(t *testing.T) {
	fpmBin := findPHPFPM()
	if fpmBin == "" {
		t.Skip("php-fpm not available")
	}

	// Keep paths short: unix socket paths are limited to ~108 chars.
	workDir, err := os.MkdirTemp("/tmp", "goupphp")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(workDir)

	root := filepath.Join(workDir, "site")
	os.MkdirAll(filepath.Join(root, "blog"), 0755)
	os.MkdirAll(filepath.Join(root, "assets"), 0755)

	writeFile := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("index.php", `<?php echo "FRONT|".$_SERVER['REQUEST_URI']."|".($_SERVER['PATH_INFO']??'')."|".$_SERVER['SCRIPT_NAME'];`)
	writeFile("about.php", `<?php echo "ABOUT";`)
	writeFile("blog/index.php", `<?php echo "BLOG";`)
	writeFile("assets/style.css", "body{}")

	// Start a private php-fpm instance on a unix socket.
	sock := filepath.Join(workDir, "fpm.sock")
	fpmConf := filepath.Join(workDir, "fpm.conf")
	confBody := fmt.Sprintf(`[global]
error_log = %s/fpm.log
daemonize = no

[www]
listen = %s
pm = static
pm.max_children = 2
`, workDir, sock)
	if err := os.WriteFile(fpmConf, []byte(confBody), 0644); err != nil {
		t.Fatal(err)
	}

	fpm := exec.Command(fpmBin, "-F", "-y", fpmConf, "-p", workDir)
	if err := fpm.Start(); err != nil {
		t.Skipf("cannot start php-fpm: %v", err)
	}
	defer func() {
		fpm.Process.Kill()
		fpm.Wait()
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(sock); err == nil {
			break
		}
		if time.Now().After(deadline) {
			log, _ := os.ReadFile(filepath.Join(workDir, "fpm.log"))
			t.Fatalf("php-fpm socket never appeared. Log: %s", log)
		}
		time.Sleep(50 * time.Millisecond)
	}

	domainLogger, err := logger.NewLogger("php-e2e-test", nil)
	if err != nil {
		t.Fatal(err)
	}

	p := &PHPPlugin{}
	if err := p.OnInit(); err != nil {
		t.Fatal(err)
	}
	siteConf := config.SiteConfig{
		Domain:        "example.test",
		RootDirectory: root,
		PluginConfigs: map[string]any{
			"PHPPlugin": map[string]any{
				"enable":   true,
				"fpm_addr": sock,
			},
		},
	}
	if err := p.OnInitForSite(siteConf, domainLogger); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		path     string
		handled  bool
		contains string
	}{
		{"Existing php file", "/about.php", true, "ABOUT"},
		{"Root serves its index.php", "/", true, "FRONT|/||/index.php"},
		{"Subdir serves its index.php", "/blog/", true, "BLOG"},
		{"Pretty permalink via front controller", "/iscrizione/", true, "FRONT|/iscrizione/|/iscrizione/|/index.php"},
		{"Pretty permalink with query", "/page?x=1", true, "FRONT|/page?x=1|/page|/index.php"},
		{"Static file falls through", "/assets/style.css", false, ""},
		// The traversal attempt is normalized under the root, so it must be
		// routed to the front controller instead of leaking fpm.conf.
		{"Traversal cannot escape root", "/../fpm.conf", true, "FRONT|"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			req.Host = "example.test"
			w := httptest.NewRecorder()

			handled := p.HandleRequest(w, req)
			if handled != tt.handled {
				t.Fatalf("handled = %v, want %v", handled, tt.handled)
			}
			if !tt.handled {
				return
			}
			resp := w.Result()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusOK {
				t.Errorf("status = %d, body: %s", resp.StatusCode, body)
			}
			if !strings.Contains(string(body), tt.contains) {
				t.Errorf("body %q does not contain %q", string(body), tt.contains)
			}
		})
	}
}
