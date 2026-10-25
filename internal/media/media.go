// Package media provides validated file storage and database metadata.
package media

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound       = errors.New("media not found")
	ErrFileTooLarge   = errors.New("file exceeds the upload size limit")
	ErrTypeNotAllowed = errors.New("file type is not allowed")
)

type Backend interface {
	Put(context.Context, string, io.Reader) error
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}

type Object struct {
	Key      string
	Modified time.Time
}

type ObjectLister interface {
	Objects(context.Context) ([]Object, error)
}

type File struct {
	ID                         uint64
	StorageBackend, StorageKey string
	OriginalName, MIMEType     string
	Size                       int64
	OwnerUserID                uint64
	Purpose                    string
	EntityType, EntityID       string
	CreatedAt                  time.Time
}

type SaveOptions struct {
	OwnerUserID          uint64
	Purpose              string
	EntityType, EntityID string
}

type Service struct {
	db           *sql.DB
	router       BackendRouter
	maxBytes     int64
	allowed      map[string]struct{}
	cleanupRules []ReferenceRule
	cleanupLock  sync.Mutex
}

func New(db *sql.DB, backend Backend, maxBytes int64, allowed []string) *Service {
	return NewRouted(db, staticRouter{backend: backend}, maxBytes, allowed)
}

func NewRouted(db *sql.DB, router BackendRouter, maxBytes int64, allowed []string) *Service {
	if maxBytes <= 0 {
		maxBytes = 10 << 20
	}
	if len(allowed) == 0 {
		allowed = []string{"image/jpeg", "image/png", "image/gif", "image/webp", "application/pdf", "text/plain"}
	}
	types := make(map[string]struct{}, len(allowed))
	for _, value := range allowed {
		types[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	return &Service{db: db, router: router, maxBytes: maxBytes, allowed: types}
}

func (service *Service) MaxBytes() int64 { return service.maxBytes }

func (service *Service) Save(ctx context.Context, originalName string, source io.Reader, options SaveOptions) (File, error) {
	buffer := make([]byte, 512)
	n, readErr := io.ReadFull(source, buffer)
	if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) && !errors.Is(readErr, io.EOF) {
		return File{}, readErr
	}
	buffer = buffer[:n]
	mimeType, _, _ := mime.ParseMediaType(http.DetectContentType(buffer))
	if _, allowed := service.allowed[mimeType]; !allowed {
		return File{}, fmt.Errorf("%w: %s", ErrTypeNotAllowed, mimeType)
	}
	limited := io.LimitReader(io.MultiReader(bytes.NewReader(buffer), source), service.maxBytes+1)
	contents, err := io.ReadAll(limited)
	if err != nil {
		return File{}, err
	}
	if int64(len(contents)) > service.maxBytes {
		return File{}, ErrFileTooLarge
	}
	if len(contents) == 0 {
		return File{}, errors.New("empty files are not accepted")
	}
	if err := validateContents(mimeType, contents); err != nil {
		return File{}, err
	}
	key, err := storageKey(mimeType, options.Purpose, time.Now())
	if err != nil {
		return File{}, err
	}
	backendName, backend, err := service.router.Current(ctx)
	if err != nil {
		return File{}, err
	}
	if err := backend.Put(ctx, key, bytes.NewReader(contents)); err != nil {
		return File{}, err
	}
	name := cleanOriginalName(originalName)
	result, err := service.db.ExecContext(ctx, `INSERT INTO media
		(storage_backend, storage_key, original_name, mime_type, size_bytes, owner_user_id, purpose, entity_type, entity_id)
		VALUES (?, ?, ?, ?, ?, NULLIF(?, 0), NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''))`,
		backendName, key, name, mimeType, len(contents), options.OwnerUserID, limit(options.Purpose, 100), limit(options.EntityType, 100), limit(options.EntityID, 100))
	if err != nil {
		_ = backend.Delete(context.Background(), key)
		return File{}, fmt.Errorf("save media metadata: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return File{}, err
	}
	return File{ID: uint64(id), StorageBackend: backendName, StorageKey: key, OriginalName: name, MIMEType: mimeType, Size: int64(len(contents)), OwnerUserID: options.OwnerUserID, Purpose: options.Purpose, EntityType: options.EntityType, EntityID: options.EntityID, CreatedAt: time.Now().UTC()}, nil
}

func validateContents(mimeType string, contents []byte) error {
	switch mimeType {
	case "image/jpeg", "image/png", "image/gif":
		if _, _, err := image.DecodeConfig(bytes.NewReader(contents)); err != nil {
			return fmt.Errorf("invalid %s image: %w", mimeType, err)
		}
	case "image/webp":
		if len(contents) < 12 || string(contents[:4]) != "RIFF" || string(contents[8:12]) != "WEBP" {
			return errors.New("invalid image/webp image")
		}
	}
	return nil
}

func (service *Service) File(ctx context.Context, id uint64) (File, error) {
	var file File
	err := service.db.QueryRowContext(ctx, `SELECT id, COALESCE(NULLIF(storage_backend,''),'local'), storage_key, original_name, mime_type, size_bytes,
		COALESCE(owner_user_id, 0), COALESCE(purpose, ''), COALESCE(entity_type, ''), COALESCE(entity_id, ''), created_at FROM media WHERE id=?`, id).
		Scan(&file.ID, &file.StorageBackend, &file.StorageKey, &file.OriginalName, &file.MIMEType, &file.Size, &file.OwnerUserID, &file.Purpose, &file.EntityType, &file.EntityID, &file.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return File{}, ErrNotFound
	}
	return file, err
}

func (service *Service) Open(ctx context.Context, id uint64) (File, io.ReadCloser, error) {
	file, err := service.File(ctx, id)
	if err != nil {
		return File{}, nil, err
	}
	backend, err := service.router.Named(ctx, file.StorageBackend)
	if err != nil {
		return File{}, nil, err
	}
	reader, err := backend.Open(ctx, file.StorageKey)
	if err != nil {
		return File{}, nil, err
	}
	return file, reader, nil
}

func (service *Service) List(ctx context.Context, query string, limit int) ([]File, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	query = strings.TrimSpace(query)
	rows, err := service.db.QueryContext(ctx, `SELECT id, COALESCE(NULLIF(storage_backend,''),'local'), storage_key, original_name, mime_type, size_bytes,
		COALESCE(owner_user_id, 0), COALESCE(purpose, ''), COALESCE(entity_type, ''), COALESCE(entity_id, ''), created_at
		FROM media WHERE (?='' OR original_name LIKE CONCAT('%', ?, '%') OR mime_type LIKE CONCAT('%', ?, '%') OR purpose LIKE CONCAT('%', ?, '%'))
		ORDER BY id DESC LIMIT ?`, query, query, query, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []File
	for rows.Next() {
		var file File
		if err := rows.Scan(&file.ID, &file.StorageBackend, &file.StorageKey, &file.OriginalName, &file.MIMEType, &file.Size, &file.OwnerUserID, &file.Purpose, &file.EntityType, &file.EntityID, &file.CreatedAt); err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, rows.Err()
}

func (service *Service) ListPage(ctx context.Context, query, purpose, date string, limit, offset int) ([]File, int, error) {
	if limit < 1 || limit > 100 {
		limit = 24
	}
	if offset < 0 {
		offset = 0
	}
	query, purpose, date = strings.TrimSpace(query), strings.TrimSpace(purpose), strings.TrimSpace(date)
	where := ` WHERE (?='' OR original_name LIKE CONCAT('%', ?, '%') OR mime_type LIKE CONCAT('%', ?, '%') OR purpose LIKE CONCAT('%', ?, '%')) AND (?='' OR COALESCE(NULLIF(purpose,''),'general')=?) AND (?='' OR DATE_FORMAT(created_at,'%d-%m-%Y')=?)`
	args := []any{query, query, query, query, purpose, purpose, date, date}
	var total int
	if err := service.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM media"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := service.db.QueryContext(ctx, `SELECT id,COALESCE(NULLIF(storage_backend,''),'local'),storage_key,original_name,mime_type,size_bytes,COALESCE(owner_user_id,0),COALESCE(purpose,''),COALESCE(entity_type,''),COALESCE(entity_id,''),created_at FROM media`+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var files []File
	for rows.Next() {
		var file File
		if err := rows.Scan(&file.ID, &file.StorageBackend, &file.StorageKey, &file.OriginalName, &file.MIMEType, &file.Size, &file.OwnerUserID, &file.Purpose, &file.EntityType, &file.EntityID, &file.CreatedAt); err != nil {
			return nil, 0, err
		}
		files = append(files, file)
	}
	return files, total, rows.Err()
}

func (service *Service) Attach(ctx context.Context, id uint64, entityType, entityID string) error {
	result, err := service.db.ExecContext(ctx, "UPDATE media SET entity_type=NULLIF(?, ''), entity_id=NULLIF(?, '') WHERE id=?", limit(entityType, 100), limit(entityID, 100), id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrNotFound
	}
	return nil
}

// SetPurpose labels how a media item is used, such as "avatar" or "post".
// An empty purpose removes the label.
func (service *Service) SetPurpose(ctx context.Context, id uint64, purpose string) error {
	return service.UpdateMetadata(ctx, id, purpose, "", "", false)
}

// UpdateMetadata changes a media item's purpose and, when updateAttachment is
// true, its application-record attachment in one database operation.
func (service *Service) UpdateMetadata(ctx context.Context, id uint64, purpose, entityType, entityID string, updateAttachment bool) error {
	query := "UPDATE media SET purpose=NULLIF(?, '') WHERE id=?"
	arguments := []any{limit(strings.TrimSpace(purpose), 100), id}
	if updateAttachment {
		query = "UPDATE media SET purpose=NULLIF(?, ''), entity_type=NULLIF(?, ''), entity_id=NULLIF(?, '') WHERE id=?"
		arguments = []any{limit(strings.TrimSpace(purpose), 100), limit(strings.TrimSpace(entityType), 100), limit(strings.TrimSpace(entityID), 100), id}
	}
	result, err := service.db.ExecContext(ctx, query, arguments...)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrNotFound
	}
	return nil
}

func (service *Service) Delete(ctx context.Context, id uint64) error {
	file, err := service.File(ctx, id)
	if err != nil {
		return err
	}
	backend, err := service.router.Named(ctx, file.StorageBackend)
	if err != nil {
		return err
	}
	if err := backend.Delete(ctx, file.StorageKey); err != nil {
		return err
	}
	_, err = service.db.ExecContext(ctx, "DELETE FROM media WHERE id=?", id)
	return err
}

func (service *Service) CleanupOrphans(ctx context.Context, minimumAge time.Duration) (int, error) {
	_, backend, err := service.router.Current(ctx)
	if err != nil {
		return 0, err
	}
	lister, ok := backend.(ObjectLister)
	if !ok {
		return 0, nil
	}
	rows, err := service.db.QueryContext(ctx, "SELECT storage_key FROM media")
	if err != nil {
		return 0, err
	}
	known := map[string]struct{}{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return 0, err
		}
		known[key] = struct{}{}
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	objects, err := lister.Objects(ctx)
	if err != nil {
		return 0, err
	}
	removed := 0
	cutoff := time.Now().Add(-minimumAge)
	for _, object := range objects {
		if _, exists := known[object.Key]; exists || object.Modified.After(cutoff) {
			continue
		}
		if err := backend.Delete(ctx, object.Key); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

func storageKey(mimeType, purpose string, uploadedAt time.Time) (string, error) {
	extensions := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", "image/webp": ".webp", "application/pdf": ".pdf", "text/plain": ".txt"}
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return strings.Join([]string{storagePurpose(purpose), uploadedAt.Format("02-01-2006"), hex.EncodeToString(buffer) + extensions[mimeType]}, "/"), nil
}

func storagePurpose(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var result strings.Builder
	separator := false
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			result.WriteRune(character)
			separator = false
		} else if result.Len() > 0 && !separator {
			result.WriteByte('-')
			separator = true
		}
	}
	cleaned := strings.Trim(result.String(), "-")
	if cleaned == "" {
		return "general"
	}
	return limit(cleaned, 100)
}

func cleanOriginalName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimSpace(strings.Map(func(character rune) rune {
		if character < 32 || character == 127 {
			return -1
		}
		return character
	}, name))
	if name == "" || name == "." {
		return "upload"
	}
	return limit(name, 255)
}

func limit(value string, maximum int) string {
	if len(value) > maximum {
		return value[:maximum]
	}
	return value
}
