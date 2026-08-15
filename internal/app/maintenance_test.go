package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMaintenanceHandlerReportsProgressFailureAndReadyState(t *testing.T) {
	handler := NewMaintenanceHandler()
	handler.Progress(25, 100)

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if got := health.Body.String(); !strings.Contains(got, `"status":"migrating"`) || !strings.Contains(got, `"completed":25`) {
		t.Fatalf("progress health = %s", got)
	}

	handler.Fail(errors.New("hash mismatch"))
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := page.Body.String(); !strings.Contains(got, "hash mismatch") || !strings.Contains(got, "original database is unchanged") {
		t.Fatalf("failure page = %s", got)
	}

	handler.Ready(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("dashboard"))
	}))
	dashboard := httptest.NewRecorder()
	handler.ServeHTTP(dashboard, httptest.NewRequest(http.MethodGet, "/", nil))
	if dashboard.Body.String() != "dashboard" {
		t.Fatalf("ready response = %q", dashboard.Body.String())
	}
}
