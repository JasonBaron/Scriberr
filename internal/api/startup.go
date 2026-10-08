package api

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

// The server binary starts HTTP before the model environments are checked,
// which can take a minute or more (each check imports the model stack). It
// calls BeginStartup first and MarkReady once jobs can run. In between,
// /health reports "starting" with 503 and API writes are refused, so nothing
// reaches the job queue before it is running. Reads, sign-in and the web UI
// work immediately.
//
// Without BeginStartup (tests, embedding) the API behaves as always ready.

var (
	starting     atomic.Bool
	startupBegan atomic.Int64 // unix nanos
	startupTook  atomic.Int64 // nanos
)

// BeginStartup puts the API in "starting" mode until MarkReady is called.
func BeginStartup() {
	startupBegan.Store(time.Now().UnixNano())
	starting.Store(true)
}

// MarkReady is called once model environments are prepared and the job queue
// has started.
func MarkReady() {
	if b := startupBegan.Load(); b != 0 {
		startupTook.Store(time.Now().UnixNano() - b)
	}
	starting.Store(false)
}

// IsReady reports whether startup has finished (always true without BeginStartup).
func IsReady() bool { return !starting.Load() }

// startupGate refuses state-changing API calls while starting. Sign-in and
// token refresh stay open so the UI can load.
func startupGate() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !starting.Load() {
			c.Next()
			return
		}
		p := c.Request.URL.Path
		m := c.Request.Method
		if !strings.HasPrefix(p, "/api/") || m == http.MethodGet || m == http.MethodHead ||
			m == http.MethodOptions || strings.HasPrefix(p, "/api/v1/auth/") {
			c.Next()
			return
		}
		c.Header("Retry-After", "15")
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
			"error":    "Scriberr is starting (preparing model environments). Try again shortly.",
			"starting": true,
		})
	}
}

// startupStatus is the /health body. Values are strings so existing clients
// that decode it as map[string]string keep working.
func startupStatus() gin.H {
	if starting.Load() {
		return gin.H{
			"status":  "starting",
			"startup": fmt.Sprintf("%ds so far", int(time.Since(time.Unix(0, startupBegan.Load())).Seconds())),
		}
	}
	h := gin.H{"status": "healthy"}
	if took := startupTook.Load(); took > 0 {
		h["startup"] = fmt.Sprintf("%ds", int(time.Duration(took).Seconds()))
	}
	return h
}
