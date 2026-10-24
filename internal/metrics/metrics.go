// Package metrics collects lightweight process and HTTP resource usage without
// an external monitoring dependency.
package metrics

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type Snapshot struct {
	StartedAt     time.Time
	Uptime        time.Duration
	MemoryBytes   uint64
	CPUPercent    float64
	Requests      uint64
	BytesReceived uint64
	BytesSent     uint64
	ReceivePerSec float64
	SendPerSec    float64
}

type Collector struct {
	startedAt time.Time
	requests  atomic.Uint64
	received  atomic.Uint64
	sent      atomic.Uint64
	mu        sync.Mutex
	lastAt    time.Time
	lastCPU   time.Duration
	cpu       float64
}

func New() *Collector {
	now := time.Now()
	_, cpu := processStats()
	return &Collector{startedAt: now, lastAt: now, lastCPU: cpu}
}

// Middleware measures HTTP body bytes handled by this process. TCP/TLS and
// HTTP header overhead are intentionally excluded.
func (collector *Collector) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		collector.requests.Add(1)
		if r.Body != nil {
			r.Body = &countingBody{ReadCloser: r.Body, count: &collector.received}
		}
		writer := &countingWriter{ResponseWriter: w, count: &collector.sent}
		next.ServeHTTP(writer, r)
		collector.sampleCPU()
	})
}

func (collector *Collector) Snapshot() Snapshot {
	collector.sampleCPU()
	now := time.Now()
	uptime := now.Sub(collector.startedAt)
	memory, _ := processStats()
	collector.mu.Lock()
	cpu := collector.cpu
	collector.mu.Unlock()
	received := collector.received.Load()
	sent := collector.sent.Load()
	seconds := uptime.Seconds()
	if seconds < 1 {
		seconds = 1
	}
	return Snapshot{
		StartedAt:     collector.startedAt,
		Uptime:        uptime,
		MemoryBytes:   memory,
		CPUPercent:    cpu,
		Requests:      collector.requests.Load(),
		BytesReceived: received,
		BytesSent:     sent,
		ReceivePerSec: float64(received) / seconds,
		SendPerSec:    float64(sent) / seconds,
	}
}

func (collector *Collector) sampleCPU() {
	_, cpu := processStats()
	now := time.Now()
	collector.mu.Lock()
	wallDelta := now.Sub(collector.lastAt)
	cpuDelta := cpu - collector.lastCPU
	if wallDelta > 0 && cpuDelta >= 0 {
		collector.cpu = cpuDelta.Seconds() / wallDelta.Seconds() * 100
	}
	collector.lastAt, collector.lastCPU = now, cpu
	collector.mu.Unlock()
}

type countingBody struct {
	io.ReadCloser
	count *atomic.Uint64
}

func (body *countingBody) Read(buffer []byte) (int, error) {
	n, err := body.ReadCloser.Read(buffer)
	body.count.Add(uint64(n))
	return n, err
}

type countingWriter struct {
	http.ResponseWriter
	count *atomic.Uint64
}

func (writer *countingWriter) Write(buffer []byte) (int, error) {
	n, err := writer.ResponseWriter.Write(buffer)
	writer.count.Add(uint64(n))
	return n, err
}

func (writer *countingWriter) Flush() {
	if flusher, ok := writer.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (writer *countingWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := writer.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func (writer *countingWriter) Push(target string, options *http.PushOptions) error {
	if pusher, ok := writer.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, options)
	}
	return http.ErrNotSupported
}
