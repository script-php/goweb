package main

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	archivepath "path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"script-php/goweb/platform/configuration"
)

type backupSummary struct {
	Files       int
	UploadFiles int
	SQLBytes    int64
}

func verifyBackup(archivePath string) (backupSummary, error) {
	reader, closeArchive, err := openBackupArchive(archivePath)
	if err != nil {
		return backupSummary{}, err
	}
	defer closeArchive()
	var summary backupSummary
	var manifest, database, uploads bool
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return summary, fmt.Errorf("read archive: %w", err)
		}
		if err := validateBackupHeader(header); err != nil {
			return summary, err
		}
		summary.Files++
		switch header.Name {
		case "manifest.txt":
			contents, err := io.ReadAll(io.LimitReader(reader, 64*1024))
			if err != nil {
				return summary, err
			}
			manifest = strings.Contains(string(contents), "format=website-backup-v1")
		case "database.sql":
			database = header.Size > 0
			summary.SQLBytes = header.Size
		case "uploads":
			uploads = header.FileInfo().IsDir()
		default:
			if strings.HasPrefix(header.Name, "uploads/") && header.Typeflag == tar.TypeReg {
				summary.UploadFiles++
			}
		}
	}
	if !manifest {
		return summary, errors.New("backup manifest is missing or unsupported")
	}
	if !database {
		return summary, errors.New("database.sql is missing or empty")
	}
	if !uploads {
		return summary, errors.New("uploads directory is missing")
	}
	return summary, nil
}

func restoreBackup(cfg config.Config, archivePath, configDirectory string) (string, error) {
	if _, err := verifyBackup(archivePath); err != nil {
		return "", err
	}
	work, err := os.MkdirTemp(configDirectory, ".restore-work-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	if err := extractBackup(archivePath, work); err != nil {
		return "", err
	}
	if err := restoreDatabase(cfg.Database, filepath.Join(work, "database.sql")); err != nil {
		return "", err
	}

	uploads := cfg.Storage.UploadsDirectory
	if uploads == "" {
		uploads = "uploads"
	}
	if !filepath.IsAbs(uploads) {
		uploads = filepath.Join(configDirectory, uploads)
	}
	previous := uploads + ".before-restore-" + time.Now().UTC().Format("20060102-150405")
	if _, err := os.Stat(uploads); err == nil {
		if err := os.Rename(uploads, previous); err != nil {
			return "", fmt.Errorf("preserve current uploads: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.Rename(filepath.Join(work, "uploads"), uploads); err != nil {
		if _, statErr := os.Stat(previous); statErr == nil {
			_ = os.Rename(previous, uploads)
		}
		return "", fmt.Errorf("restore uploads: %w", err)
	}
	return previous, nil
}

func extractBackup(archivePath, destination string) error {
	reader, closeArchive, err := openBackupArchive(archivePath)
	if err != nil {
		return err
	}
	defer closeArchive()
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := validateBackupHeader(header); err != nil {
			return err
		}
		target := filepath.Join(destination, filepath.FromSlash(header.Name))
		if header.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o750); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(header.Mode)&0o700)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(file, reader)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
}

func restoreDatabase(database config.Database, sqlPath string) error {
	client, err := exec.LookPath("mysql")
	if err != nil {
		client, err = exec.LookPath("mariadb")
	}
	if err != nil {
		return fmt.Errorf("find mysql or mariadb client: %w", err)
	}
	input, err := os.Open(sqlPath)
	if err != nil {
		return err
	}
	defer input.Close()
	command := exec.Command(client, "--host="+database.Host, "--port="+strconv.Itoa(database.Port), "--user="+database.User, database.Name)
	command.Env = append(os.Environ(), "MYSQL_PWD="+database.Password)
	command.Stdin, command.Stdout, command.Stderr = input, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("restore database: %w", err)
	}
	return nil
}

func openBackupArchive(archivePath string) (*tar.Reader, func(), error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return nil, func() {}, err
	}
	gz, err := gzip.NewReader(file)
	if err != nil {
		file.Close()
		return nil, func() {}, fmt.Errorf("open gzip archive: %w", err)
	}
	return tar.NewReader(gz), func() { _ = gz.Close(); _ = file.Close() }, nil
}

func validateBackupHeader(header *tar.Header) error {
	clean := archivepath.Clean(header.Name)
	if clean == "." || clean != header.Name || archivepath.IsAbs(header.Name) || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("unsafe archive path %q", header.Name)
	}
	if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeDir {
		return fmt.Errorf("unsupported archive entry %q", header.Name)
	}
	return nil
}
