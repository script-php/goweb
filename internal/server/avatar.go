package server

import (
	"database/sql"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	appmedia "script-php/goweb/internal/media"
)

func registerPublicAvatars(mux *http.ServeMux, db *sql.DB, media *appmedia.Service) {
	mux.HandleFunc("GET /avatars/{userID}", func(w http.ResponseWriter, r *http.Request) {
		userID, err := strconv.ParseUint(r.PathValue("userID"), 10, 64)
		if err != nil || userID == 0 {
			http.NotFound(w, r)
			return
		}
		var mediaID uint64
		if err := db.QueryRowContext(r.Context(), "SELECT COALESCE(avatar_media_id,0) FROM users WHERE id=? AND active=1", userID).Scan(&mediaID); err != nil || mediaID == 0 {
			http.NotFound(w, r)
			return
		}
		file, reader, err := media.Open(r.Context(), mediaID)
		if errors.Is(err, appmedia.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "Internal Server Error", 500)
			return
		}
		defer reader.Close()
		w.Header().Set("Content-Type", file.MIMEType)
		w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
		w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": file.OriginalName}))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = io.Copy(w, reader)
	})
}
