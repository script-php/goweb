package clientip

import (
	"net/http/httptest"
	"testing"
)

func TestUntrustedPeerCannotSpoofForwardedAddress(t *testing.T) {
	resolver, _ := New([]string{"10.0.0.0/8"})
	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "192.0.2.10:1234"
	request.Header.Set("X-Forwarded-For", "203.0.113.5")
	if got := resolver.Resolve(request); got != "192.0.2.10" {
		t.Fatalf("Resolve() = %q", got)
	}
}

func TestTrustedProxyChainReturnsOrigin(t *testing.T) {
	resolver, _ := New([]string{"10.0.0.0/8"})
	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "10.0.0.2:1234"
	request.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")
	if got := resolver.Resolve(request); got != "203.0.113.5" {
		t.Fatalf("Resolve() = %q", got)
	}
}

func TestInvalidTrustedProxyIsRejected(t *testing.T) {
	if _, err := New([]string{"not-an-address"}); err == nil {
		t.Fatal("invalid trusted proxy accepted")
	}
}
