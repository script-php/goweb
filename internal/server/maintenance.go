package server

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

const maintenancePage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex"><title>Temporarily unavailable</title>
<link rel="stylesheet" href="/static/css/app.css"></head>
<body><main><p class="eyebrow">MAINTENANCE</p><h1>We’ll be back shortly.</h1>
<p>The website cannot reach its database right now. Please try again in a moment.</p></main></body></html>`

type readinessCache struct {
	db       databasePinger
	interval time.Duration
	mu       sync.Mutex
	checked  time.Time
	ready    bool
}

func (cache *readinessCache) isReady(parent context.Context) bool {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if !cache.checked.IsZero() && time.Since(cache.checked) < cache.interval {
		return cache.ready
	}
	cache.ready = databaseReady(parent, cache.db)
	cache.checked = time.Now()
	return cache.ready
}

func databaseMaintenance(db databasePinger, interval time.Duration, next http.Handler) http.Handler {
	cache := &readinessCache{db: db, interval: interval}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/health") || strings.HasPrefix(r.URL.Path, "/static/") || cache.isReady(r.Context()) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(maintenancePage))
	})
}
