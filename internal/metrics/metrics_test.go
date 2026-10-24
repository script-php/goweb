package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddlewareCountsHTTPBodyBytes(t *testing.T) {
	collector := New()
	handler := collector.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buffer := make([]byte, 16)
		_, _ = r.Body.Read(buffer)
		_, _ = w.Write([]byte("response"))
	}))
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("request"))
	handler.ServeHTTP(httptest.NewRecorder(), request)
	snapshot := collector.Snapshot()
	if snapshot.Requests != 1 || snapshot.BytesReceived != 7 || snapshot.BytesSent != 8 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
}
