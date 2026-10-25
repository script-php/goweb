package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakePinger struct{ err error }

func (pinger fakePinger) PingContext(context.Context) error { return pinger.err }

func TestHealthLiveDoesNotRequireDatabase(t *testing.T) {
	response := httptest.NewRecorder()
	healthLive("1.2.3").ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"live":true`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestHealthReadyReportsDatabaseFailure(t *testing.T) {
	response := httptest.NewRecorder()
	healthReady(fakePinger{err: errors.New("offline")}, "1.2.3", NewReadiness()).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"ready":false`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestHealthSummaryReportsReady(t *testing.T) {
	response := httptest.NewRecorder()
	healthSummary(fakePinger{}, "1.2.3", NewReadiness()).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"ready":true`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestHealthReadyStopsAcceptingTrafficDuringShutdown(t *testing.T) {
	lifecycle := NewReadiness()
	lifecycle.SetReady(false)
	response := httptest.NewRecorder()
	healthReady(fakePinger{}, "1.2.3", lifecycle).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"ready":false`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}
