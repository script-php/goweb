package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteUploadsIncludesFilesAndSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "images"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "images", "photo.txt"), []byte("image"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "images", "photo.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	if err := writeUploads(writer, root); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(&buffer)
	found := false
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Name == "uploads/link.txt" {
			t.Fatal("symlink was archived")
		}
		if header.Name == "uploads/images/photo.txt" {
			contents, _ := io.ReadAll(reader)
			found = string(contents) == "image"
		}
	}
	if !found {
		t.Fatal("uploaded file was not archived")
	}
}

func TestVerifyBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup.tar.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	writer := tar.NewWriter(gz)
	if err := writeArchiveBytes(writer, "manifest.txt", []byte("format=website-backup-v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeArchiveBytes(writer, "database.sql", []byte("CREATE TABLE example (id INT);"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteHeader(&tar.Header{Name: "uploads", Typeflag: tar.TypeDir, Mode: 0o750}); err != nil {
		t.Fatal(err)
	}
	if err := writeArchiveBytes(writer, "uploads/file.txt", []byte("upload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	summary, err := verifyBackup(path)
	if err != nil {
		t.Fatal(err)
	}
	if summary.UploadFiles != 1 || summary.SQLBytes == 0 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestPruneBackupsKeepsNewest(t *testing.T) {
	directory := t.TempDir()
	var newest string
	for index := 0; index < 4; index++ {
		path := filepath.Join(directory, "website-"+string(rune('a'+index))+".tar.gz")
		if err := os.WriteFile(path, []byte("backup"), 0o600); err != nil {
			t.Fatal(err)
		}
		modified := time.Now().Add(time.Duration(index) * time.Minute)
		if err := os.Chtimes(path, modified, modified); err != nil {
			t.Fatal(err)
		}
		newest = path
	}
	if err := pruneBackups(directory, "website-", 2, newest); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("kept %d backups, want 2", len(entries))
	}
	if _, err := os.Stat(newest); err != nil {
		t.Fatal("newest backup was removed")
	}
}
