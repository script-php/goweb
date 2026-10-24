package server

import (
	"database/sql"
	"fmt"
	"net/http"

	"script-php/goweb/internal/metrics"
)

func registerPrometheusMetrics(mux *http.ServeMux, collector *metrics.Collector, db *sql.DB) {
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		snapshot := collector.Snapshot()
		stats := db.Stats()
		var mediaBytes, jobFailures uint64
		_ = db.QueryRowContext(r.Context(), "SELECT COALESCE(SUM(size_bytes),0) FROM media").Scan(&mediaBytes)
		_ = db.QueryRowContext(r.Context(), "SELECT COALESCE(SUM(failure_count),0) FROM background_jobs").Scan(&jobFailures)
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		fmt.Fprintf(w, "# TYPE goweb_process_memory_bytes gauge\ngoweb_process_memory_bytes %d\n", snapshot.MemoryBytes)
		fmt.Fprintf(w, "# TYPE goweb_process_cpu_percent gauge\ngoweb_process_cpu_percent %.3f\n", snapshot.CPUPercent)
		fmt.Fprintf(w, "# TYPE goweb_http_requests_total counter\ngoweb_http_requests_total %d\n", snapshot.Requests)
		fmt.Fprintf(w, "# TYPE goweb_http_received_bytes_total counter\ngoweb_http_received_bytes_total %d\n", snapshot.BytesReceived)
		fmt.Fprintf(w, "# TYPE goweb_http_sent_bytes_total counter\ngoweb_http_sent_bytes_total %d\n", snapshot.BytesSent)
		fmt.Fprintf(w, "# TYPE goweb_uptime_seconds gauge\ngoweb_uptime_seconds %.0f\n", snapshot.Uptime.Seconds())
		fmt.Fprintf(w, "# TYPE goweb_database_open_connections gauge\ngoweb_database_open_connections %d\n", stats.OpenConnections)
		fmt.Fprintf(w, "# TYPE goweb_database_in_use_connections gauge\ngoweb_database_in_use_connections %d\n", stats.InUse)
		fmt.Fprintf(w, "# TYPE goweb_database_idle_connections gauge\ngoweb_database_idle_connections %d\n", stats.Idle)
		fmt.Fprintf(w, "# TYPE goweb_media_storage_bytes gauge\ngoweb_media_storage_bytes %d\n", mediaBytes)
		fmt.Fprintf(w, "# TYPE goweb_background_job_failures_total counter\ngoweb_background_job_failures_total %d\n", jobFailures)
	})
}
