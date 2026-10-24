package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"script-php/goweb/platform"
	"script-php/goweb/platform/configuration"
)

func backupAdminOperations(cfg config.Config, configDirectory, version string) *platform.BackupOperations {
	root := filepath.Join(configDirectory, "backups")
	safePath := func(name string) (string, error) {
		if filepath.Base(name) != name || !strings.HasSuffix(name, ".tar.gz") {
			return "", fmt.Errorf("invalid backup name")
		}
		return filepath.Join(root, name), nil
	}
	return &platform.BackupOperations{
		List: func(context.Context) ([]platform.BackupFile, error) {
			entries, err := os.ReadDir(root)
			if errors.Is(err, os.ErrNotExist) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			var result []platform.BackupFile
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tar.gz") {
					continue
				}
				info, err := entry.Info()
				if err != nil {
					return nil, err
				}
				result = append(result, platform.BackupFile{Name: entry.Name(), Size: info.Size(), Modified: info.ModTime()})
			}
			sort.Slice(result, func(i, j int) bool { return result[i].Modified.After(result[j].Modified) })
			return result, nil
		},
		Create: func(context.Context) (platform.BackupFile, error) {
			path, err := createBackup(cfg.Database, cfg.Storage, cfg.Backup, root, configDirectory, version)
			if err != nil {
				return platform.BackupFile{}, err
			}
			info, err := os.Stat(path)
			if err != nil {
				return platform.BackupFile{}, err
			}
			return platform.BackupFile{Name: filepath.Base(path), Size: info.Size(), Modified: info.ModTime()}, nil
		},
		Open: func(_ context.Context, name string) (io.ReadCloser, error) {
			path, err := safePath(name)
			if err != nil {
				return nil, err
			}
			return os.Open(path)
		},
		Verify: func(_ context.Context, name string) error {
			path, err := safePath(name)
			if err != nil {
				return err
			}
			_, err = verifyBackup(path)
			return err
		},
	}
}

// createBackup creates one archive containing the database and uploaded files.
func createBackup(database config.Database, storage config.Storage, policy config.Backup, directory, configDirectory, version string) (string, error) {
	if directory == "" {
		directory = "backups"
	}
	if !filepath.IsAbs(directory) {
		directory = filepath.Join(configDirectory, directory)
	}
	backupDirectory, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(backupDirectory, 0o700); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}
	work, err := os.MkdirTemp(backupDirectory, ".backup-work-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	if err := dumpDatabase(database, filepath.Join(work, "database.sql")); err != nil {
		return "", err
	}
	uploads := storage.UploadsDirectory
	if uploads == "" {
		uploads = "uploads"
	}
	if !filepath.IsAbs(uploads) {
		uploads = filepath.Join(configDirectory, uploads)
	}
	if err := os.MkdirAll(uploads, 0o750); err != nil {
		return "", fmt.Errorf("prepare uploads directory: %w", err)
	}

	archivePath := filepath.Join(backupDirectory, database.Name+"-"+time.Now().UTC().Format("20060102-150405")+".tar.gz")
	archive, err := os.OpenFile(archivePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	success := false
	defer func() {
		_ = archive.Close()
		if !success {
			_ = os.Remove(archivePath)
		}
	}()
	gz := gzip.NewWriter(archive)
	tw := tar.NewWriter(gz)
	manifest := "format=website-backup-v1\nversion=" + version + "\ncreated_at=" + time.Now().UTC().Format(time.RFC3339) + "\n"
	if err := writeArchiveBytes(tw, "manifest.txt", []byte(manifest), 0o600); err != nil {
		return "", err
	}
	if err := writeArchiveFile(tw, filepath.Join(work, "database.sql"), "database.sql"); err != nil {
		return "", err
	}
	if err := writeUploads(tw, uploads); err != nil {
		return "", err
	}
	if err := tw.Close(); err != nil {
		return "", err
	}
	if err := gz.Close(); err != nil {
		return "", err
	}
	if err := archive.Close(); err != nil {
		return "", err
	}
	success = true
	if policy.RetentionCount > 0 {
		if err := pruneBackups(backupDirectory, database.Name+"-", policy.RetentionCount, archivePath); err != nil {
			return "", fmt.Errorf("prune old backups: %w", err)
		}
	}
	return archivePath, nil
}

func pruneBackups(directory, prefix string, keep int, current string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	type candidate struct {
		path     string
		modified time.Time
	}
	var backups []candidate
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), ".tar.gz") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		backups = append(backups, candidate{path: filepath.Join(directory, entry.Name()), modified: info.ModTime()})
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].modified.After(backups[j].modified) })
	for index := keep; index < len(backups); index++ {
		if samePath(backups[index].path, current) {
			continue
		}
		if err := os.Remove(backups[index].path); err != nil {
			return err
		}
	}
	return nil
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && leftAbs == rightAbs
}

func dumpDatabase(database config.Database, outputPath string) error {
	dumper, err := exec.LookPath("mysqldump")
	if err != nil {
		dumper, err = exec.LookPath("mariadb-dump")
	}
	if err != nil {
		return fmt.Errorf("find mysqldump or mariadb-dump: %w", err)
	}
	output, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	command := exec.Command(dumper, "--host="+database.Host, "--port="+strconv.Itoa(database.Port), "--user="+database.User,
		"--single-transaction", "--no-tablespaces", "--routines", "--triggers", "--events", "--default-character-set=utf8mb4", database.Name)
	command.Env = append(os.Environ(), "MYSQL_PWD="+database.Password)
	command.Stdout, command.Stderr = output, os.Stderr
	runErr, closeErr := command.Run(), output.Close()
	if runErr != nil {
		return fmt.Errorf("run database backup: %w", runErr)
	}
	return closeErr
}

func writeUploads(writer *tar.Writer, root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := "uploads"
		if relative != "." {
			name += "/" + filepath.ToSlash(relative)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = name
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func writeArchiveFile(writer *tar.Writer, path, name string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = name
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(writer, file)
	return err
}

func writeArchiveBytes(writer *tar.Writer, name string, contents []byte, mode int64) error {
	header := &tar.Header{Name: strings.TrimPrefix(filepath.ToSlash(name), "/"), Mode: mode, Size: int64(len(contents)), ModTime: time.Now().UTC()}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	_, err := writer.Write(contents)
	return err
}
