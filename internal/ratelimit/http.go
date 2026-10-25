package ratelimit

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

var validScope = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,119}$`)

type Policy struct {
	ID     string
	Limit  int
	Window time.Duration
	Match  func(*http.Request) bool
	Key    func(*http.Request) string
}

func ValidatePolicies(policies []Policy) error {
	seen := map[string]bool{}
	for _, policy := range policies {
		if !validScope.MatchString(policy.ID) || policy.Limit < 1 || policy.Window <= 0 || policy.Match == nil || policy.Key == nil {
			return fmt.Errorf("invalid rate-limit policy %q", policy.ID)
		}
		if seen[policy.ID] {
			return fmt.Errorf("duplicate rate-limit policy %q", policy.ID)
		}
		seen[policy.ID] = true
	}
	return nil
}

func (store *Store) Middleware(policies []Policy, responder func(http.ResponseWriter, *http.Request, int), next http.Handler) http.Handler {
	if responder == nil {
		responder = func(w http.ResponseWriter, _ *http.Request, status int) {
			http.Error(w, http.StatusText(status), status)
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, policy := range policies {
			if !policy.Match(r) {
				continue
			}
			key := policy.Key(r)
			if key == "" {
				continue
			}
			// Hit blocks at its threshold. Adding one makes an HTTP policy allow
			// exactly Limit requests and reject the following request.
			decision, err := store.Hit(r.Context(), "http."+policy.ID, key, policy.Limit+1, policy.Window, time.Now())
			if err != nil {
				responder(w, r, http.StatusInternalServerError)
				return
			}
			if !decision.Allowed {
				seconds := int((decision.RetryAfter + time.Second - 1) / time.Second)
				if seconds < 1 {
					seconds = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(seconds))
				responder(w, r, http.StatusTooManyRequests)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
