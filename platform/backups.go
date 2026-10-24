package platform

import (
	"context"
	"io"
	"time"

	serveradmin "script-php/goweb/internal/server/admin"
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

func backupOperationsAdapter(source *BackupOperations) *serveradmin.BackupOperations {
	return &serveradmin.BackupOperations{
		List: func(ctx context.Context) ([]serveradmin.BackupFile, error) {
			items, err := source.List(ctx)
			result := make([]serveradmin.BackupFile, len(items))
			for i, item := range items {
				result[i] = serveradmin.BackupFile{Name: item.Name, Size: item.Size, Modified: item.Modified}
			}
			return result, err
		},
		Create: func(ctx context.Context) (serveradmin.BackupFile, error) {
			item, err := source.Create(ctx)
			return serveradmin.BackupFile{Name: item.Name, Size: item.Size, Modified: item.Modified}, err
		},
		Open: source.Open, Verify: source.Verify,
	}
}
