package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

var safeKey = regexp.MustCompile(`^(?:[a-z0-9][a-z0-9-]{0,99}/\d{2}-\d{2}-\d{4}/)?[a-f0-9]{48}\.(jpg|png|gif|webp|pdf|txt)$`)

type LocalBackend struct{ root string }

func NewLocalBackend(root string) (*LocalBackend, error) {
	if root == "" {
		root = "uploads"
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absolute, 0o750); err != nil {
		return nil, err
	}
	return &LocalBackend{root: absolute}, nil
}

func (backend *LocalBackend) path(key string) (string, error) {
	if !safeKey.MatchString(key) {
		return "", fmt.Errorf("unsafe media storage key %q", key)
	}
	return filepath.Join(backend.root, filepath.FromSlash(key)), nil
}

func (backend *LocalBackend) Put(_ context.Context, key string, source io.Reader) error {
	target, err := backend.path(key)
	if err != nil {
		return err
	}
	directory := filepath.Dir(target)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".upload-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o640); err != nil {
		temporary.Close()
		return err
	}
	if _, err := io.Copy(temporary, source); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if _, err := os.Stat(target); err == nil {
		return errors.New("media storage key already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(temporaryPath, target)
}

func (backend *LocalBackend) Open(_ context.Context, key string) (io.ReadCloser, error) {
	path, err := backend.path(key)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (backend *LocalBackend) Delete(_ context.Context, key string) error {
	path, err := backend.path(key)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (backend *LocalBackend) Objects(_ context.Context) ([]Object, error) {
	var objects []Object
	err := filepath.WalkDir(backend.root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(backend.root, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(relative)
		if !safeKey.MatchString(key) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		objects = append(objects, Object{Key: key, Modified: info.ModTime()})
		return nil
	})
	return objects, err
}
