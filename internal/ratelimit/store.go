// Package ratelimit provides counters shared through the application database.
package ratelimit

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Decision struct {
	Allowed    bool
	Hits       int
	RetryAfter time.Duration
}

type entry struct {
	hits         int
	windowStart  time.Time
	blockedUntil time.Time
}

type Store struct {
	db     *sql.DB
	mu     sync.Mutex
	memory map[string]entry
}

func New(db *sql.DB) *Store { return &Store{db: db, memory: map[string]entry{}} }

// Check reports an existing block without consuming another attempt.
func (store *Store) Check(ctx context.Context, scope, key string, now time.Time) (Decision, error) {
	if store.db == nil {
		store.mu.Lock()
		defer store.mu.Unlock()
		value := store.memory[scope+"|"+key]
		return existingDecision(value, now), nil
	}
	var hits int
	var blocked sql.NullTime
	err := store.db.QueryRowContext(ctx, `SELECT hits, blocked_until FROM rate_limits WHERE scope=? AND key_hash=?`, scope, hash(key)).Scan(&hits, &blocked)
	if errors.Is(err, sql.ErrNoRows) {
		return Decision{Allowed: true}, nil
	}
	if err != nil {
		return Decision{}, err
	}
	decision := Decision{Allowed: true, Hits: hits}
	if blocked.Valid && now.Before(blocked.Time) {
		decision.Allowed = false
		decision.RetryAfter = blocked.Time.Sub(now)
	}
	return decision, nil
}

// Hit atomically consumes an attempt in a fixed window shared by every process.
func (store *Store) Hit(ctx context.Context, scope, key string, limit int, window time.Duration, now time.Time) (Decision, error) {
	if scope == "" || key == "" || limit < 1 || window <= 0 {
		return Decision{}, errors.New("rate limit requires a scope, key, positive limit, and window")
	}
	if store.db == nil {
		return store.hitMemory(scope, key, limit, window, now), nil
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return Decision{}, err
	}
	defer tx.Rollback()
	keyHash := hash(key)
	if _, err := tx.ExecContext(ctx, `INSERT INTO rate_limits (scope,key_hash,hits,window_started_at) VALUES (?,?,0,?) ON DUPLICATE KEY UPDATE key_hash=VALUES(key_hash)`, scope, keyHash, now.UTC()); err != nil {
		return Decision{}, err
	}
	var value entry
	var blocked sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT hits,window_started_at,blocked_until FROM rate_limits WHERE scope=? AND key_hash=? FOR UPDATE`, scope, keyHash).Scan(&value.hits, &value.windowStart, &blocked); err != nil {
		return Decision{}, err
	}
	if blocked.Valid {
		value.blockedUntil = blocked.Time
	}
	if !value.blockedUntil.IsZero() && now.Before(value.blockedUntil) {
		if _, err := tx.ExecContext(ctx, `UPDATE rate_limits SET limited_count=limited_count+1,last_limited_at=? WHERE scope=? AND key_hash=?`, now.UTC(), scope, keyHash); err != nil {
			return Decision{}, err
		}
		if err := tx.Commit(); err != nil {
			return Decision{}, err
		}
		return Decision{Allowed: false, Hits: value.hits, RetryAfter: value.blockedUntil.Sub(now)}, nil
	}
	if now.Sub(value.windowStart) >= window {
		value.hits = 0
		value.windowStart = now.UTC()
	}
	value.hits++
	decision := Decision{Allowed: value.hits < limit, Hits: value.hits}
	var blockedUntil any
	limitedIncrement := 0
	var lastLimited any
	if !decision.Allowed {
		value.blockedUntil = now.Add(window).UTC()
		blockedUntil = value.blockedUntil
		lastLimited = now.UTC()
		limitedIncrement = 1
		decision.RetryAfter = window
	}
	_, err = tx.ExecContext(ctx, `UPDATE rate_limits SET hits=?,window_started_at=?,blocked_until=?,limited_count=limited_count+?,last_limited_at=COALESCE(?,last_limited_at) WHERE scope=? AND key_hash=?`, value.hits, value.windowStart, blockedUntil, limitedIncrement, lastLimited, scope, keyHash)
	if err != nil {
		return Decision{}, err
	}
	if err := tx.Commit(); err != nil {
		return Decision{}, err
	}
	return decision, nil
}

func (store *Store) Reset(ctx context.Context, scope, key string) error {
	if store.db == nil {
		store.mu.Lock()
		delete(store.memory, scope+"|"+key)
		store.mu.Unlock()
		return nil
	}
	_, err := store.db.ExecContext(ctx, `DELETE FROM rate_limits WHERE scope=? AND key_hash=?`, scope, hash(key))
	return err
}

func (store *Store) Cleanup(ctx context.Context, olderThan time.Time) error {
	if store.db == nil {
		return nil
	}
	_, err := store.db.ExecContext(ctx, `DELETE FROM rate_limits WHERE updated_at < ? AND (blocked_until IS NULL OR blocked_until < ?)`, olderThan.UTC(), time.Now().UTC())
	return err
}

func (store *Store) hitMemory(scope, key string, limit int, window time.Duration, now time.Time) Decision {
	store.mu.Lock()
	defer store.mu.Unlock()
	memoryKey := scope + "|" + key
	value := store.memory[memoryKey]
	if !value.blockedUntil.IsZero() && now.Before(value.blockedUntil) {
		return Decision{Allowed: false, Hits: value.hits, RetryAfter: value.blockedUntil.Sub(now)}
	}
	if value.windowStart.IsZero() || now.Sub(value.windowStart) >= window {
		value = entry{windowStart: now}
	}
	value.hits++
	decision := Decision{Allowed: value.hits < limit, Hits: value.hits}
	if !decision.Allowed {
		value.blockedUntil = now.Add(window)
		decision.RetryAfter = window
	}
	store.memory[memoryKey] = value
	return decision
}

func existingDecision(value entry, now time.Time) Decision {
	decision := Decision{Allowed: true, Hits: value.hits}
	if !value.blockedUntil.IsZero() && now.Before(value.blockedUntil) {
		decision.Allowed = false
		decision.RetryAfter = value.blockedUntil.Sub(now)
	}
	return decision
}

func hash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func (decision Decision) String() string {
	return fmt.Sprintf("allowed=%t hits=%d retry_after=%s", decision.Allowed, decision.Hits, decision.RetryAfter)
}
