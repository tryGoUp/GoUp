package dashboard

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDatastarPatchElements(t *testing.T) {
	rec := httptest.NewRecorder()
	err := datastarPatchElements(rec, `<span id="live-sites-count">1</span>`)
	if err != nil {
		t.Fatalf("datastarPatchElements returned error: %v", err)
	}

	body := rec.Body.String()
	checks := []string{
		"event: datastar-patch-elements\n",
		"data: elements <span id=\"live-sites-count\">1</span>\n",
		"\n\n",
	}
	for _, check := range checks {
		if !strings.Contains(body, check) {
			t.Fatalf("expected stream to contain %q, got %q", check, body)
		}
	}
}

func TestStreamDashboardHandlerWritesDatastarEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/datastar/dashboard", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		streamDashboardHandler(rec, req)
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("streamDashboardHandler did not stop after context cancel")
	}

	body := rec.Body.String()
	if !strings.Contains(body, "event: datastar-patch-elements\n") {
		t.Fatalf("expected Datastar event, got %q", body)
	}
	if !rec.Flushed {
		t.Fatal("expected stream to flush")
	}
}

func TestDashboardFragmentsContainDatastarTargets(t *testing.T) {
	fragments := dashboardFragments()
	targets := []string{
		`id="live-sites-count"`,
		`id="live-vhosts-count"`,
		`id="live-log-weight"`,
		`id="live-home-metrics"`,
		`id="metricsList"`,
		`id="pluginUsageList"`,
		`id="sitesList"`,
		`id="liveLogs"`,
	}
	for _, target := range targets {
		if !strings.Contains(fragments, target) {
			t.Fatalf("expected fragments to contain %s", target)
		}
	}
}
