package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type databasePinger interface{ PingContext(context.Context) error }

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Live    bool   `json:"live"`
	Ready   *bool  `json:"ready,omitempty"`
}

func healthLive(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeHealth(w, http.StatusOK, healthResponse{Status: "ok", Version: version, Live: true})
	}
}

func healthReady(db databasePinger, version string, lifecycle *Readiness) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ready := lifecycle.IsReady() && databaseReady(r.Context(), db)
		status, state := http.StatusOK, "ready"
		if !ready {
			status, state = http.StatusServiceUnavailable, "unavailable"
		}
		writeHealth(w, status, healthResponse{Status: state, Version: version, Live: true, Ready: &ready})
	}
}

func healthSummary(db databasePinger, version string, lifecycle *Readiness) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ready := lifecycle.IsReady() && databaseReady(r.Context(), db)
		status, state := http.StatusOK, "ok"
		if !ready {
			status, state = http.StatusServiceUnavailable, "degraded"
		}
		writeHealth(w, status, healthResponse{Status: state, Version: version, Live: true, Ready: &ready})
	}
}

func databaseReady(parent context.Context, db databasePinger) bool {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	return db.PingContext(ctx) == nil
}

func writeHealth(w http.ResponseWriter, status int, response healthResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}
