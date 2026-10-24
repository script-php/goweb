// Package jobs runs registered periodic work and persists operational status.
package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"sync"
	"time"
)

var safeID = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,119}$`)
var ErrRunning = errors.New("background job is already running")

type Job struct {
	ID, Label string
	Interval  time.Duration
	Enabled   bool
	Run       func(context.Context) error
}

type Status struct {
	ID, Label, State, LastError        string
	Enabled                            bool
	Interval                           time.Duration
	LastStarted, LastFinished, NextRun sql.NullTime
	LastDuration                       time.Duration
	RunCount, FailureCount             uint64
}

type Manager struct {
	db      *sql.DB
	jobs    map[string]Job
	mu      sync.Mutex
	running map[string]bool
}

func New(db *sql.DB, registered []Job) (*Manager, error) {
	manager := &Manager{db: db, jobs: map[string]Job{}, running: map[string]bool{}}
	for _, job := range registered {
		if !safeID.MatchString(job.ID) || job.Run == nil {
			return nil, fmt.Errorf("invalid background job %q", job.ID)
		}
		if _, exists := manager.jobs[job.ID]; exists {
			return nil, fmt.Errorf("duplicate background job %q", job.ID)
		}
		if job.Label == "" {
			job.Label = job.ID
		}
		if job.Interval < time.Minute {
			job.Interval = time.Hour
		}
		manager.jobs[job.ID] = job
		_, err := db.Exec(`INSERT IGNORE INTO background_jobs (job_id,label,enabled,interval_seconds,next_run_at) VALUES (?,?,?,?,DATE_ADD(UTC_TIMESTAMP(), INTERVAL ? SECOND))`, job.ID, job.Label, job.Enabled, int(job.Interval.Seconds()), int(job.Interval.Seconds()))
		if err != nil {
			return nil, fmt.Errorf("register job %s: %w", job.ID, err)
		}
		_, _ = db.Exec(`UPDATE background_jobs SET label=? WHERE job_id=?`, job.Label, job.ID)
	}
	return manager, nil
}

func (manager *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	manager.runDue(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			manager.runDue(ctx)
		}
	}
}

func (manager *Manager) runDue(ctx context.Context) {
	rows, err := manager.db.QueryContext(ctx, `SELECT job_id FROM background_jobs WHERE enabled=1 AND (next_run_at IS NULL OR next_run_at<=UTC_TIMESTAMP())`)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	_ = rows.Close()
	for _, id := range ids {
		go func(jobID string) { _ = manager.RunNow(ctx, jobID) }(id)
	}
}

func (manager *Manager) RunNow(ctx context.Context, id string) error {
	job, exists := manager.jobs[id]
	if !exists {
		return sql.ErrNoRows
	}
	manager.mu.Lock()
	if manager.running[id] {
		manager.mu.Unlock()
		return ErrRunning
	}
	manager.running[id] = true
	manager.mu.Unlock()
	defer func() { manager.mu.Lock(); delete(manager.running, id); manager.mu.Unlock() }()
	started := time.Now()
	_, _ = manager.db.ExecContext(ctx, `UPDATE background_jobs SET status='running',last_started_at=UTC_TIMESTAMP(),last_error=NULL WHERE job_id=?`, id)
	err := job.Run(ctx)
	duration := time.Since(started)
	state := "success"
	errorText := ""
	failed := 0
	if err != nil {
		state = "failed"
		errorText = err.Error()
		failed = 1
	}
	_, updateErr := manager.db.ExecContext(context.WithoutCancel(ctx), `UPDATE background_jobs SET status=?,last_finished_at=UTC_TIMESTAMP(),last_duration_ms=?,last_error=NULLIF(?,''),run_count=run_count+1,failure_count=failure_count+?,next_run_at=DATE_ADD(UTC_TIMESTAMP(), INTERVAL interval_seconds SECOND) WHERE job_id=?`, state, duration.Milliseconds(), errorText, failed, id)
	if err != nil {
		return err
	}
	return updateErr
}

func (manager *Manager) List(ctx context.Context) ([]Status, error) {
	rows, err := manager.db.QueryContext(ctx, `SELECT job_id,label,enabled,interval_seconds,status,last_started_at,last_finished_at,last_duration_ms,COALESCE(last_error,''),run_count,failure_count,next_run_at FROM background_jobs ORDER BY label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Status
	for rows.Next() {
		var s Status
		var seconds, ms int64
		if err := rows.Scan(&s.ID, &s.Label, &s.Enabled, &seconds, &s.State, &s.LastStarted, &s.LastFinished, &ms, &s.LastError, &s.RunCount, &s.FailureCount, &s.NextRun); err != nil {
			return nil, err
		}
		s.Interval = time.Duration(seconds) * time.Second
		s.LastDuration = time.Duration(ms) * time.Millisecond
		result = append(result, s)
	}
	return result, rows.Err()
}

func (manager *Manager) Configure(ctx context.Context, id string, enabled bool, interval time.Duration) error {
	if _, ok := manager.jobs[id]; !ok {
		return sql.ErrNoRows
	}
	if interval < time.Minute {
		return errors.New("job interval must be at least one minute")
	}
	_, err := manager.db.ExecContext(ctx, `UPDATE background_jobs SET enabled=?,interval_seconds=?,next_run_at=DATE_ADD(UTC_TIMESTAMP(), INTERVAL ? SECOND) WHERE job_id=?`, enabled, int(interval.Seconds()), int(interval.Seconds()), id)
	return err
}
func (manager *Manager) IDs() []string {
	result := make([]string, 0, len(manager.jobs))
	for id := range manager.jobs {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}
