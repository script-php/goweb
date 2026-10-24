package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDatabaseMaintenanceServesFriendlyPage(t *testing.T) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("application handler was called") })
	response := httptest.NewRecorder()
	databaseMaintenance(fakePinger{err: errors.New("offline")}, time.Second, next).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/account", nil))
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "30" {
		t.Fatalf("response = %d headers=%v", response.Code, response.Header())
	}
	if !strings.Contains(response.Body.String(), "back shortly") {
		t.Fatalf("maintenance body = %q", response.Body.String())
	}
}

func TestDatabaseMaintenanceAllowsHealthyAndInfrastructureRequests(t *testing.T) {
	for _, test := range []struct {
		path string
		db   fakePinger
	}{{"/", fakePinger{}}, {"/health/ready", fakePinger{err: errors.New("offline")}}, {"/static/css/app.css", fakePinger{err: errors.New("offline")}}} {
		called := false
		next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) })
		response := httptest.NewRecorder()
		databaseMaintenance(test.db, time.Second, next).ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if !called || response.Code != http.StatusNoContent {
			t.Errorf("path %s was blocked: called=%v status=%d", test.path, called, response.Code)
		}
	}
}
