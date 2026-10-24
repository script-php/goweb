package platform

import (
	"context"
	"io"
	"strconv"
	"time"

	appmedia "script-php/goweb/internal/media"
)

func AvatarURL(userID uint64) string {
	if userID == 0 {
		return ""
	}
	return "/avatars/" + strconv.FormatUint(userID, 10)
}

var (
	ErrMediaNotFound       = appmedia.ErrNotFound
	ErrMediaFileTooLarge   = appmedia.ErrFileTooLarge
	ErrMediaTypeNotAllowed = appmedia.ErrTypeNotAllowed
	ErrMediaCleanupRunning = appmedia.ErrCleanupRunning
)

// StorageBackend allows applications to replace local media storage with an
// object store while the platform continues to own validation and metadata.
type StorageBackend interface {
	Put(context.Context, string, io.Reader) error
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}

type MediaObject struct {
	Key      string
	Modified time.Time
}

// StorageObjectLister is optional but required for bidirectional cleanup.
type StorageObjectLister interface {
	Objects(context.Context) ([]MediaObject, error)
}

type listingStorageBackend struct {
	StorageBackend
	lister StorageObjectLister
}

func (backend listingStorageBackend) Objects(ctx context.Context) ([]appmedia.Object, error) {
	objects, err := backend.lister.Objects(ctx)
	result := make([]appmedia.Object, len(objects))
	for index, object := range objects {
		result[index] = appmedia.Object{Key: object.Key, Modified: object.Modified}
	}
	return result, err
}

type MediaFile struct {
	ID                                 uint64
	StorageKey, OriginalName, MIMEType string
	Size                               int64
	OwnerUserID                        uint64
	Purpose                            string
	EntityType, EntityID               string
	CreatedAt                          time.Time
}

type MediaOptions struct {
	OwnerUserID          uint64
	Purpose              string
	EntityType, EntityID string
}

// MediaReferenceRule declares a nullable application column that can contain
// media.id. Rules are registered in code and can be enabled by administrators.
type MediaReferenceRule struct {
	ID, Label, Table, Column string
}

type MediaCleanupOptions struct {
	DryRun         bool
	MinimumAge     time.Duration
	EnabledRuleIDs []string
	RemoveUnused   bool
}

type MediaCleanupResult struct {
	DryRun                                      bool
	OrphanFiles, MissingFiles, MediaRowsRemoved int
	ReferencesCleared                           int64
	UnusedMedia                                 int
}

type Media struct{ inner *appmedia.Service }

func (media *Media) Save(ctx context.Context, originalName string, source io.Reader, options MediaOptions) (MediaFile, error) {
	file, err := media.inner.Save(ctx, originalName, source, appmedia.SaveOptions{OwnerUserID: options.OwnerUserID, Purpose: options.Purpose, EntityType: options.EntityType, EntityID: options.EntityID})
	return publicMediaFile(file), err
}

func (media *Media) Open(ctx context.Context, id uint64) (MediaFile, io.ReadCloser, error) {
	file, reader, err := media.inner.Open(ctx, id)
	return publicMediaFile(file), reader, err
}

func (media *Media) List(ctx context.Context, query string, limit int) ([]MediaFile, error) {
	files, err := media.inner.List(ctx, query, limit)
	result := make([]MediaFile, len(files))
	for index, file := range files {
		result[index] = publicMediaFile(file)
	}
	return result, err
}

func (media *Media) Attach(ctx context.Context, id uint64, entityType, entityID string) error {
	return media.inner.Attach(ctx, id, entityType, entityID)
}

// SetPurpose labels how a media item is used, such as "avatar" or "post".
func (media *Media) SetPurpose(ctx context.Context, id uint64, purpose string) error {
	return media.inner.SetPurpose(ctx, id, purpose)
}

// UpdateMetadata changes purpose and record attachment together.
func (media *Media) UpdateMetadata(ctx context.Context, id uint64, purpose, entityType, entityID string) error {
	return media.inner.UpdateMetadata(ctx, id, purpose, entityType, entityID, true)
}

func (media *Media) Delete(ctx context.Context, id uint64) error { return media.inner.Delete(ctx, id) }

func (media *Media) Cleanup(ctx context.Context, options MediaCleanupOptions) (MediaCleanupResult, error) {
	result, err := media.inner.Cleanup(ctx, appmedia.CleanupOptions{DryRun: options.DryRun, MinimumAge: options.MinimumAge, RuleIDs: options.EnabledRuleIDs, RemoveUnused: options.RemoveUnused})
	return MediaCleanupResult(result), err
}

func publicMediaFile(file appmedia.File) MediaFile {
	return MediaFile{ID: file.ID, StorageKey: file.StorageKey, OriginalName: file.OriginalName, MIMEType: file.MIMEType, Size: file.Size, OwnerUserID: file.OwnerUserID, Purpose: file.Purpose, EntityType: file.EntityType, EntityID: file.EntityID, CreatedAt: file.CreatedAt}
}
