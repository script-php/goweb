//go:build integration

package jobs

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func TestJobLeasePreventsCrossInstanceOverlapIntegration(t *testing.T) {
	dsn := os.Getenv("WEBSITE_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("WEBSITE_INTEGRATION_DSN is not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const id = "integration.cross_instance_lease"
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM background_jobs WHERE job_id=?`, id) })
	started := make(chan struct{})
	release := make(chan struct{})
	runs := make(chan struct{}, 2)
	var signalStarted sync.Once
	job := Job{ID: id, Interval: time.Hour, Enabled: true, Run: func(ctx context.Context) error {
		runs <- struct{}{}
		signalStarted.Do(func() { close(started) })
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	first, err := New(db, []Job{job})
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(db, []Job{job})
	if err != nil {
		t.Fatal(err)
	}
	firstResult := make(chan error, 1)
	go func() { firstResult <- first.RunNow(context.Background(), id) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first job did not start")
	}
	if err := second.RunNow(context.Background(), id); !errors.Is(err, ErrRunning) {
		t.Fatalf("second RunNow() error = %v, want ErrRunning", err)
	}
	close(release)
	if err := <-firstResult; err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("job ran %d times, want 1", len(runs))
	}
	var lease sql.NullString
	if err := db.QueryRow(`SELECT lease_token FROM background_jobs WHERE job_id=?`, id).Scan(&lease); err != nil {
		t.Fatal(err)
	}
	if lease.Valid {
		t.Fatalf("completed job retained lease %q", lease.String)
	}
}

func TestJobRecoversExpiredLeaseIntegration(t *testing.T) {
	dsn := os.Getenv("WEBSITE_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("WEBSITE_INTEGRATION_DSN is not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const id = "integration.expired_lease"
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM background_jobs WHERE job_id=?`, id) })
	runs := 0
	manager, err := New(db, []Job{{ID: id, Interval: time.Hour, Enabled: true, Run: func(context.Context) error {
		runs++
		return nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE background_jobs SET lease_token='abandoned',lease_expires_at=DATE_SUB(UTC_TIMESTAMP(), INTERVAL 1 SECOND) WHERE job_id=?`, id); err != nil {
		t.Fatal(err)
	}
	if err := manager.RunNow(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if runs != 1 {
		t.Fatalf("job ran %d times, want 1", runs)
	}
}
