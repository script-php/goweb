package media

import (
	"errors"
	"testing"
)

func TestValidateContentsRejectsMalformedImages(t *testing.T) {
	for _, mimeType := range []string{"image/jpeg", "image/png", "image/gif", "image/webp"} {
		if err := validateContents(mimeType, []byte("not a real image")); err == nil {
			t.Errorf("malformed %s accepted", mimeType)
		}
	}
}

func TestMediaErrorsRemainComparable(t *testing.T) {
	if !errors.Is(ErrFileTooLarge, ErrFileTooLarge) || !errors.Is(ErrTypeNotAllowed, ErrTypeNotAllowed) {
		t.Fatal("public media errors are not comparable")
	}
}
