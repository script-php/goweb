package server

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"script-php/goweb/internal/auth"
	"script-php/goweb/internal/clientip"
)

type requestIDKey struct{}

func RequestID(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDKey{}).(string)
	return requestID
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func requestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := RequestID(r.Context())
		observer := &responseObserver{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(observer, r)

		userID := uint64(0)
		if user := auth.CurrentUser(r.Context()); user != nil {
			userID = user.ID
		}
		slog.Info("http request", "request_id", requestID, "method", r.Method, "path", r.URL.RequestURI(), "status", observer.status, "duration_ms", time.Since(started).Milliseconds(), "bytes", observer.bytes, "client_ip", clientip.FromRequest(r), "user_id", userID)
	})
}

func identifyRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := cleanRequestID(r.Header.Get("X-Request-ID"))
		if requestID == "" {
			requestID = newRequestID()
		}
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, requestID)))
	})
}

func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("panic recovered", "request_id", RequestID(r.Context()), "method", r.Method, "path", r.URL.RequestURI(), "panic", recovered, "stack", string(debug.Stack()))
				if state, ok := w.(interface{ Written() bool }); ok && state.Written() {
					return
				}
				auth.RespondError(w, r, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type responseObserver struct {
	http.ResponseWriter
	status  int
	bytes   int64
	written bool
}

func (writer *responseObserver) WriteHeader(status int) {
	if writer.written {
		return
	}
	writer.status, writer.written = status, true
	writer.ResponseWriter.WriteHeader(status)
}
func (writer *responseObserver) Write(buffer []byte) (int, error) {
	if !writer.written {
		writer.WriteHeader(http.StatusOK)
	}
	n, err := writer.ResponseWriter.Write(buffer)
	writer.bytes += int64(n)
	return n, err
}
func (writer *responseObserver) Written() bool               { return writer.written }
func (writer *responseObserver) Unwrap() http.ResponseWriter { return writer.ResponseWriter }
func (writer *responseObserver) Flush() {
	if !writer.written {
		writer.WriteHeader(http.StatusOK)
	}
	if flusher, ok := writer.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
func (writer *responseObserver) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := writer.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}
func (writer *responseObserver) Push(target string, options *http.PushOptions) error {
	if pusher, ok := writer.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, options)
	}
	return http.ErrNotSupported
}

func cleanRequestID(value string) string {
	if len(value) == 0 || len(value) > 64 {
		return ""
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || strings.ContainsRune("-_.", character)) {
			return ""
		}
	}
	return value
}
func newRequestID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "unavailable"
	}
	return hex.EncodeToString(buffer)
}
