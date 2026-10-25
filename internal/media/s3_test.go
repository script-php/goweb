package media

import (
	"testing"

	config "script-php/goweb/platform/configuration"
)

func TestS3BackendNormalizesPrefixAndRejectsUnsafeKeys(t *testing.T) {
	backend, err := NewS3Backend(config.S3{Endpoint: "https://minio.test:9000", Bucket: "media", AccessKey: "access", SecretKey: "secret", UseTLS: true, Prefix: "/sites/demo/"})
	if err != nil {
		t.Fatal(err)
	}
	key := "avatar/25-10-2026/0123456789abcdef0123456789abcdef0123456789abcdef.png"
	object, err := backend.objectKey(key)
	if err != nil || object != "sites/demo/"+key {
		t.Fatalf("objectKey() = %q, %v", object, err)
	}
	if _, err := backend.objectKey("../secret"); err == nil {
		t.Fatal("unsafe object key was accepted")
	}
}
