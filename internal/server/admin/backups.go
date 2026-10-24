package admin

import (
	"context"
	"io"
	"time"
)

type BackupFile struct {
	Name     string
	Size     int64
	Modified time.Time
}
type BackupOperations struct {
	List   func(context.Context) ([]BackupFile, error)
	Create func(context.Context) (BackupFile, error)
	Open   func(context.Context, string) (io.ReadCloser, error)
	Verify func(context.Context, string) error
}
