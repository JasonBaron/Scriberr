package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestStartupGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(startupGate())
	ok := func(c *gin.Context) { c.Status(http.StatusOK) }
	r.GET("/api/v1/transcription/list", ok)
	r.POST("/api/v1/transcription/x/start", ok)
	r.POST("/api/v1/auth/login", ok)
	r.GET("/", ok)

	cases := []struct {
		method, path string
		starting     int // status while starting
	}{
		{"GET", "/api/v1/transcription/list", 200},
		{"POST", "/api/v1/transcription/x/start", 503},
		{"POST", "/api/v1/auth/login", 200},
		{"GET", "/", 200},
	}
	run := func(m, p string) int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(m, p, nil))
		return w.Code
	}

	// Default (no BeginStartup): always ready, as tests and embedders expect
	for _, c := range cases {
		if got := run(c.method, c.path); got != 200 {
			t.Errorf("default: %s %s = %d, want 200", c.method, c.path, got)
		}
	}

	BeginStartup()
	if s := startupStatus(); s["status"] != "starting" {
		t.Errorf("status while starting = %v", s)
	}
	for _, c := range cases {
		if got := run(c.method, c.path); got != c.starting {
			t.Errorf("starting: %s %s = %d, want %d", c.method, c.path, got, c.starting)
		}
	}
	MarkReady()
	for _, c := range cases {
		if got := run(c.method, c.path); got != 200 {
			t.Errorf("ready: %s %s = %d, want 200", c.method, c.path, got)
		}
	}
	if s := startupStatus(); s["status"] != "healthy" || s["startup"] == nil {
		t.Errorf("status after ready = %v", s)
	}
}
