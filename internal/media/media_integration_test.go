//go:build integration

package media

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func TestMediaLifecycleIntegration(t *testing.T) {
	dsn := os.Getenv("WEBSITE_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("WEBSITE_INTEGRATION_DSN is not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	backend, err := NewLocalBackend(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := New(db, backend, 1024, []string{"image/png"})
	ctx := context.Background()
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	file, err := service.Save(ctx, `..\unsafe\avatar.png`, strings.NewReader(string(png)), SaveOptions{Purpose: "avatar", EntityType: "user", EntityID: "42"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Delete(context.Background(), file.ID) })
	if file.OriginalName != "avatar.png" || file.MIMEType != "image/png" || file.Purpose != "avatar" || file.EntityID != "42" {
		t.Fatalf("saved file = %#v", file)
	}
	loaded, reader, err := service.Open(ctx, file.ID)
	if err != nil {
		t.Fatal(err)
	}
	contents, _ := io.ReadAll(reader)
	reader.Close()
	if loaded.ID != file.ID || string(contents) != string(png) {
		t.Fatal("opened media does not match saved contents")
	}
	if err := service.Attach(ctx, file.ID, "article", "100"); err != nil {
		t.Fatal(err)
	}
	if err := service.SetPurpose(ctx, file.ID, "post"); err != nil {
		t.Fatal(err)
	}
	files, err := service.List(ctx, "post", 10)
	if err != nil || len(files) != 1 || files[0].Purpose != "post" || files[0].EntityType != "article" {
		t.Fatalf("media list = %#v err=%v", files, err)
	}
	if _, err := service.Save(ctx, "fake.png", strings.NewReader("<html>not an image</html>"), SaveOptions{}); !errors.Is(err, ErrTypeNotAllowed) {
		t.Fatalf("disguised executable content was accepted: %v", err)
	}
	if _, err := service.Save(ctx, "large.png", strings.NewReader(string(png)+strings.Repeat("x", 2048)), SaveOptions{}); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("oversized file was accepted: %v", err)
	}
	if err := service.Delete(ctx, file.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.File(ctx, file.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted metadata still exists: %v", err)
	}
}

func TestMediaOrphanCleanupIntegration(t *testing.T) {
	dsn := os.Getenv("WEBSITE_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("WEBSITE_INTEGRATION_DSN is not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	backend, err := NewLocalBackend(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("b", 48) + ".txt"
	if err := backend.Put(context.Background(), key, strings.NewReader("orphan")); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(backend.root+string(os.PathSeparator)+key, old, old); err != nil {
		t.Fatal(err)
	}
	removed, err := New(db, backend, 1024, nil).CleanupOrphans(context.Background(), 24*time.Hour)
	if err != nil || removed != 1 {
		t.Fatalf("CleanupOrphans() = %d, %v", removed, err)
	}
}

func TestBidirectionalCleanupAndReferencesIntegration(t *testing.T) {
	dsn := os.Getenv("WEBSITE_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("WEBSITE_INTEGRATION_DSN is not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS media_cleanup_test`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE media_cleanup_test (id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT, media_id BIGINT UNSIGNED NULL)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(context.Background(), "DROP TABLE IF EXISTS media_cleanup_test") })
	backend, err := NewLocalBackend(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// The integration database may contain application media from earlier jobs
	// or local development. Mirror those objects into this test backend so this
	// cleanup run observes only the missing row created below.
	rows, err := db.QueryContext(ctx, "SELECT storage_key FROM media")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if err := backend.Put(ctx, key, strings.NewReader("existing test object")); err != nil {
			rows.Close()
			t.Fatal(err)
		}
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	service := New(db, backend, 1024, []string{"text/plain"})
	if err := service.ConfigureCleanupRules([]ReferenceRule{{ID: "test_media", Table: "media_cleanup_test", Column: "media_id"}}); err != nil {
		t.Fatal(err)
	}
	missing, err := service.Save(ctx, "missing.txt", strings.NewReader("missing"), SaveOptions{Purpose: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO media_cleanup_test (media_id) VALUES (?)", missing.ID); err != nil {
		t.Fatal(err)
	}
	if err := backend.Delete(ctx, missing.StorageKey); err != nil {
		t.Fatal(err)
	}
	orphanKey := "test/24-10-2026/" + strings.Repeat("c", 48) + ".txt"
	if err := backend.Put(ctx, orphanKey, strings.NewReader("orphan")); err != nil {
		t.Fatal(err)
	}
	orphanPath, _ := backend.path(orphanKey)
	old := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(orphanPath, old, old)
	preview, err := service.Cleanup(ctx, CleanupOptions{DryRun: true, MinimumAge: 24 * time.Hour, RuleIDs: []string{"test_media"}})
	if err != nil || preview.OrphanFiles != 1 || preview.MediaRowsRemoved != 1 || preview.ReferencesCleared != 1 {
		t.Fatalf("preview = %#v, err=%v", preview, err)
	}
	var reference sql.NullInt64
	if err := db.QueryRowContext(ctx, "SELECT media_id FROM media_cleanup_test LIMIT 1").Scan(&reference); err != nil || !reference.Valid {
		t.Fatalf("preview changed reference: %v, %v", reference, err)
	}
	result, err := service.Cleanup(ctx, CleanupOptions{MinimumAge: 24 * time.Hour, RuleIDs: []string{"test_media"}})
	if err != nil || result.OrphanFiles != 1 || result.MediaRowsRemoved != 1 || result.ReferencesCleared != 1 {
		t.Fatalf("cleanup = %#v, err=%v", result, err)
	}
	if err := db.QueryRowContext(ctx, "SELECT media_id FROM media_cleanup_test LIMIT 1").Scan(&reference); err != nil || reference.Valid {
		t.Fatalf("reference not cleared: %v, %v", reference, err)
	}
	if _, err := service.File(ctx, missing.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing-file row remains: %v", err)
	}
}
