package server

import (
	"database/sql"
	"encoding/json"
	"html/template"
	"io/fs"
	"net/http"
	"time"
)

type application struct {
	db      *sql.DB
	pages   *template.Template
	version string
}

func New(webFiles fs.FS, db *sql.DB, version string) (http.Handler, error) {
	pages, err := template.ParseFS(webFiles, "templates/*.html")
	if err != nil {
		return nil, err
	}

	staticFiles, err := fs.Sub(webFiles, "static")
	if err != nil {
		return nil, err
	}

	app := &application{db: db, pages: pages, version: version}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", app.home)
	mux.HandleFunc("GET /healthz", app.health)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFiles))))
	return securityHeaders(mux), nil
}

func (app *application) home(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Version string
	}{Version: app.version}
	if err := app.pages.ExecuteTemplate(w, "index.html", data); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (app *application) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, 2*time.Second)
	defer cancel()

	w.Header().Set("Content-Type", "application/json")
	if err := app.db.PingContext(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "unavailable"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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
