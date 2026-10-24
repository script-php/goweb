package media

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalBackendRejectsUnsafeKeys(t *testing.T) {
	backend, err := NewLocalBackend(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"../secret.txt", "/tmp/file.jpg", "nested/file.png", "avatar/../../file.png", "bad.exe"} {
		if err := backend.Put(context.Background(), key, strings.NewReader("value")); err == nil {
			t.Errorf("unsafe key %q accepted", key)
		}
	}
}

func TestLocalBackendNestedRoundTrip(t *testing.T) {
	backend, err := NewLocalBackend(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := "avatar/24-10-2026/" + strings.Repeat("b", 48) + ".jpg"
	if err := backend.Put(context.Background(), key, strings.NewReader("avatar")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(backend.root, filepath.FromSlash(key))); err != nil {
		t.Fatal(err)
	}
	objects, err := backend.Objects(context.Background())
	if err != nil || len(objects) != 1 || objects[0].Key != key {
		t.Fatalf("objects = %#v, err = %v", objects, err)
	}
}

func TestStorageKeyUsesPurposeAndDate(t *testing.T) {
	key, err := storageKey("image/png", "User Avatar / Unsafe", time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "user-avatar-unsafe/24-10-2026/") || !strings.HasSuffix(key, ".png") {
		t.Fatalf("storageKey() = %q", key)
	}
	key, err = storageKey("image/png", "", time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC))
	if err != nil || !strings.HasPrefix(key, "general/24-10-2026/") {
		t.Fatalf("fallback storageKey() = %q, err = %v", key, err)
	}
}

func TestLocalBackendRoundTrip(t *testing.T) {
	backend, err := NewLocalBackend(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("a", 48) + ".png"
	if err := backend.Put(context.Background(), key, strings.NewReader("contents")); err != nil {
		t.Fatal(err)
	}
	reader, err := backend.Open(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	contents, _ := io.ReadAll(reader)
	reader.Close()
	if string(contents) != "contents" {
		t.Fatalf("contents = %q", contents)
	}
	if err := backend.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(backend.root + string(os.PathSeparator) + key); !os.IsNotExist(err) {
		t.Fatalf("deleted object still exists: %v", err)
	}
}

func TestOriginalFilenameIsSanitized(t *testing.T) {
	if got := cleanOriginalName(`..\private\photo.jpg`); got != "photo.jpg" {
		t.Fatalf("cleanOriginalName() = %q", got)
	}
}
