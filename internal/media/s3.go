package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	configuration "script-php/goweb/platform/configuration"
)

type S3Backend struct {
	client *minio.Client
	bucket string
	prefix string
}

func NewS3Backend(cfg configuration.S3) (*S3Backend, error) {
	endpoint := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(cfg.Endpoint, "https://"), "http://"))
	if endpoint == "" || cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("S3 endpoint, bucket, access key, and secret key are required")
	}
	options := &minio.Options{Creds: credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""), Secure: cfg.UseTLS, Region: cfg.Region}
	if cfg.PathStyle {
		options.BucketLookup = minio.BucketLookupPath
	}
	client, err := minio.New(endpoint, options)
	if err != nil {
		return nil, fmt.Errorf("create S3 client: %w", err)
	}
	return &S3Backend{client: client, bucket: cfg.Bucket, prefix: strings.Trim(strings.TrimSpace(cfg.Prefix), "/")}, nil
}

func (backend *S3Backend) objectKey(key string) (string, error) {
	if !safeKey.MatchString(key) {
		return "", fmt.Errorf("unsafe media storage key %q", key)
	}
	if backend.prefix == "" {
		return key, nil
	}
	return backend.prefix + "/" + key, nil
}

func (backend *S3Backend) Check(ctx context.Context, create bool, region string) error {
	exists, err := backend.client.BucketExists(ctx, backend.bucket)
	if err != nil {
		return fmt.Errorf("check S3 bucket: %w", err)
	}
	if exists {
		return nil
	}
	if !create {
		return fmt.Errorf("S3 bucket %q does not exist", backend.bucket)
	}
	if err := backend.client.MakeBucket(ctx, backend.bucket, minio.MakeBucketOptions{Region: region}); err != nil {
		return fmt.Errorf("create S3 bucket: %w", err)
	}
	return nil
}

func (backend *S3Backend) Put(ctx context.Context, key string, source io.Reader) error {
	objectKey, err := backend.objectKey(key)
	if err != nil {
		return err
	}
	_, err = backend.client.PutObject(ctx, backend.bucket, objectKey, source, -1, minio.PutObjectOptions{ContentType: "application/octet-stream"})
	return err
}

func (backend *S3Backend) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	objectKey, err := backend.objectKey(key)
	if err != nil {
		return nil, err
	}
	object, err := backend.client.GetObject(ctx, backend.bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	if _, err := object.Stat(); err != nil {
		object.Close()
		return nil, err
	}
	return object, nil
}

func (backend *S3Backend) Delete(ctx context.Context, key string) error {
	objectKey, err := backend.objectKey(key)
	if err != nil {
		return err
	}
	return backend.client.RemoveObject(ctx, backend.bucket, objectKey, minio.RemoveObjectOptions{})
}

func (backend *S3Backend) Objects(ctx context.Context) ([]Object, error) {
	prefix := backend.prefix
	if prefix != "" {
		prefix += "/"
	}
	var result []Object
	for object := range backend.client.ListObjects(ctx, backend.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if object.Err != nil {
			return nil, object.Err
		}
		key := strings.TrimPrefix(object.Key, prefix)
		if safeKey.MatchString(key) {
			result = append(result, Object{Key: key, Modified: object.LastModified})
		}
	}
	return result, nil
}
