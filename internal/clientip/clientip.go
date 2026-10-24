// Package clientip safely resolves the originating client address when the
// application is behind explicitly trusted reverse proxies.
package clientip

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type contextKey struct{}

type Resolver struct {
	trusted []netip.Prefix
}

func New(trustedProxies []string) (*Resolver, error) {
	resolver := &Resolver{}
	for _, value := range trustedProxies {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if address, err := netip.ParseAddr(value); err == nil {
			bits := 128
			if address.Is4() {
				bits = 32
			}
			resolver.trusted = append(resolver.trusted, netip.PrefixFrom(address, bits))
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy %q", value)
		}
		resolver.trusted = append(resolver.trusted, prefix)
	}
	return resolver, nil
}

func (resolver *Resolver) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := resolver.Resolve(r)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, ip)))
	})
}

func FromRequest(r *http.Request) string {
	if value, ok := r.Context().Value(contextKey{}).(string); ok && value != "" {
		return value
	}
	return remoteHost(r.RemoteAddr)
}

func (resolver *Resolver) Resolve(r *http.Request) string {
	peer := remoteHost(r.RemoteAddr)
	peerAddress, err := netip.ParseAddr(peer)
	if err != nil || !resolver.isTrusted(peerAddress) {
		return peer
	}
	forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for index := len(forwarded) - 1; index >= 0; index-- {
		candidate := strings.TrimSpace(forwarded[index])
		address, err := netip.ParseAddr(candidate)
		if err != nil {
			continue
		}
		if !resolver.isTrusted(address) || index == 0 {
			return address.String()
		}
	}
	if realIP, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-IP"))); err == nil {
		return realIP.String()
	}
	return peer
}

func (resolver *Resolver) isTrusted(address netip.Addr) bool {
	for _, prefix := range resolver.trusted {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func remoteHost(remoteAddress string) string {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err == nil {
		return host
	}
	return remoteAddress
}
