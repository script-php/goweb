package mail

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

const maxAttempts = 5

type Queue struct {
	db      *sql.DB
	deliver Sender
	wake    chan struct{}
}

func NewQueue(db *sql.DB, deliver Sender) *Queue {
	return &Queue{db: db, deliver: deliver, wake: make(chan struct{}, 1)}
}

// Send stores mail durably. The worker handles SMTP delivery separately from
// the request that produced the message.
func (queue *Queue) Send(ctx context.Context, message Message) error {
	if strings.TrimSpace(message.To) == "" || strings.TrimSpace(message.Subject) == "" {
		return errors.New("email recipient and subject are required")
	}
	_, err := queue.db.ExecContext(ctx, `INSERT INTO email_queue
		(recipient, subject, body, status, next_attempt_at) VALUES (?, ?, ?, 'queued', UTC_TIMESTAMP())`,
		message.To, limitText(message.Subject, 255), message.Text)
	if err != nil {
		return fmt.Errorf("queue email: %w", err)
	}
	select {
	case queue.wake <- struct{}{}:
	default:
	}
	return nil
}

// Run processes queued messages until the context is cancelled.
func (queue *Queue) Run(ctx context.Context) {
	// A process may have stopped after claiming a message. Make those messages
	// eligible again; this application runs one worker per database.
	if _, err := queue.db.ExecContext(ctx, `UPDATE email_queue SET status='queued', next_attempt_at=UTC_TIMESTAMP() WHERE status='sending'`); err != nil {
		slog.Error("recover interrupted email deliveries", "error", err)
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	queue.processAvailable(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			queue.processAvailable(ctx)
		case <-queue.wake:
			queue.processAvailable(ctx)
		}
	}
}

func (queue *Queue) Cleanup(ctx context.Context) error {
	days := 90
	var configured string
	if err := queue.db.QueryRowContext(ctx, "SELECT setting_value FROM settings WHERE setting_key='mail.retention_days'").Scan(&configured); err == nil {
		if candidate, parseErr := strconv.Atoi(configured); parseErr == nil && candidate >= 1 && candidate <= 3650 {
			days = candidate
		}
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	_, err := queue.db.ExecContext(ctx, `DELETE FROM email_queue WHERE status IN ('sent','failed') AND updated_at < ?`, cutoff)
	return err
}

func (queue *Queue) processAvailable(ctx context.Context) {
	for ctx.Err() == nil {
		item, ok, err := queue.claim(ctx)
		if err != nil {
			slog.Error("claim queued email", "error", err)
			return
		}
		if !ok {
			return
		}
		deliveryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = queue.deliver.Send(deliveryCtx, Message{To: item.recipient, Subject: item.subject, Text: item.body})
		cancel()
		if err == nil {
			if _, updateErr := queue.db.ExecContext(ctx, `UPDATE email_queue SET status='sent', sent_at=UTC_TIMESTAMP(), next_attempt_at=NULL, last_error=NULL, body='' WHERE id=?`, item.id); updateErr != nil {
				slog.Error("record email delivery", "id", item.id, "error", updateErr)
			}
			continue
		}
		status := "queued"
		var next any = time.Now().UTC().Add(retryDelay(item.attempts))
		if item.attempts >= maxAttempts {
			status = "failed"
			next = nil
		}
		if _, updateErr := queue.db.ExecContext(ctx, `UPDATE email_queue SET status=?, next_attempt_at=?, last_error=? WHERE id=?`, status, next, limitText(err.Error(), 2000), item.id); updateErr != nil {
			slog.Error("record email failure", "id", item.id, "error", updateErr)
		}
		slog.Warn("email delivery failed", "id", item.id, "attempt", item.attempts, "error", err)
	}
}

type queuedItem struct {
	id                       uint64
	recipient, subject, body string
	attempts                 int
}

func (queue *Queue) claim(ctx context.Context) (queuedItem, bool, error) {
	tx, err := queue.db.BeginTx(ctx, nil)
	if err != nil {
		return queuedItem{}, false, err
	}
	defer tx.Rollback()
	var item queuedItem
	err = tx.QueryRowContext(ctx, `SELECT id, recipient, subject, body, attempts FROM email_queue
		WHERE status='queued' AND (next_attempt_at IS NULL OR next_attempt_at <= UTC_TIMESTAMP()) ORDER BY id LIMIT 1 FOR UPDATE`).
		Scan(&item.id, &item.recipient, &item.subject, &item.body, &item.attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return queuedItem{}, false, nil
	}
	if err != nil {
		return queuedItem{}, false, err
	}
	item.attempts++
	result, err := tx.ExecContext(ctx, `UPDATE email_queue SET status='sending', attempts=? WHERE id=? AND status='queued'`, item.attempts, item.id)
	if err != nil {
		return queuedItem{}, false, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return queuedItem{}, false, nil
	}
	if err := tx.Commit(); err != nil {
		return queuedItem{}, false, err
	}
	return item, true, nil
}

func retryDelay(attempt int) time.Duration {
	delays := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}
	if attempt <= 0 {
		return delays[0]
	}
	if attempt > len(delays) {
		return delays[len(delays)-1]
	}
	return delays[attempt-1]
}

func limitText(value string, maximum int) string {
	if len(value) > maximum {
		return value[:maximum]
	}
	return value
}
