package media

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var safeIdentifier = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
var ErrCleanupRunning = errors.New("media cleanup is already running")

type ReferenceRule struct {
	ID, Label, Table, Column string
}

type CleanupOptions struct {
	DryRun       bool
	MinimumAge   time.Duration
	RuleIDs      []string
	RemoveUnused bool
}

type CleanupResult struct {
	DryRun                                      bool
	OrphanFiles, MissingFiles, MediaRowsRemoved int
	ReferencesCleared                           int64
	UnusedMedia                                 int
}

func (service *Service) ConfigureCleanupRules(rules []ReferenceRule) error {
	seen := map[string]struct{}{}
	for index := range rules {
		rule := &rules[index]
		rule.ID, rule.Table, rule.Column = strings.TrimSpace(rule.ID), strings.TrimSpace(rule.Table), strings.TrimSpace(rule.Column)
		if rule.ID == "" {
			rule.ID = rule.Table + "." + rule.Column
		}
		if rule.Label == "" {
			rule.Label = rule.ID
		}
		if !safeIdentifier.MatchString(rule.Table) || !safeIdentifier.MatchString(rule.Column) || !safeIdentifier.MatchString(strings.ReplaceAll(rule.ID, ".", "_")) {
			return fmt.Errorf("invalid media reference rule %q", rule.ID)
		}
		if _, exists := seen[rule.ID]; exists {
			return fmt.Errorf("duplicate media reference rule %q", rule.ID)
		}
		seen[rule.ID] = struct{}{}
	}
	service.cleanupRules = append([]ReferenceRule(nil), rules...)
	return nil
}

func (service *Service) ReferenceRules() []ReferenceRule {
	return append([]ReferenceRule(nil), service.cleanupRules...)
}

func (service *Service) Cleanup(ctx context.Context, options CleanupOptions) (CleanupResult, error) {
	result := CleanupResult{DryRun: options.DryRun}
	if !service.cleanupLock.TryLock() {
		return result, ErrCleanupRunning
	}
	defer service.cleanupLock.Unlock()
	if options.MinimumAge < 0 {
		options.MinimumAge = 0
	}
	lister, ok := service.backend.(ObjectLister)
	if !ok {
		return result, fmt.Errorf("media backend does not support object listing")
	}
	objects, err := lister.Objects(ctx)
	if err != nil {
		return result, err
	}
	objectMap := make(map[string]Object, len(objects))
	for _, object := range objects {
		objectMap[object.Key] = object
	}
	rows, err := service.db.QueryContext(ctx, "SELECT id, storage_key, created_at FROM media")
	if err != nil {
		return result, err
	}
	type record struct {
		id      uint64
		key     string
		created time.Time
	}
	var records []record
	known := map[string]struct{}{}
	for rows.Next() {
		var item record
		if err := rows.Scan(&item.id, &item.key, &item.created); err != nil {
			rows.Close()
			return result, err
		}
		records, known[item.key] = append(records, item), struct{}{}
	}
	if err := rows.Close(); err != nil {
		return result, err
	}
	cutoff := time.Now().Add(-options.MinimumAge)
	for _, object := range objects {
		if _, exists := known[object.Key]; exists || object.Modified.After(cutoff) {
			continue
		}
		result.OrphanFiles++
		if !options.DryRun {
			if err := service.backend.Delete(ctx, object.Key); err != nil {
				return result, err
			}
		}
	}
	rules := service.selectedRules(options.RuleIDs)
	for _, item := range records {
		if _, exists := objectMap[item.key]; exists {
			continue
		}
		result.MissingFiles++
		if options.DryRun {
			cleared, err := countReferences(ctx, service.db, rules, item.id)
			if err != nil {
				return result, err
			}
			result.ReferencesCleared += cleared
			result.MediaRowsRemoved++
			continue
		}
		tx, err := service.db.BeginTx(ctx, nil)
		if err != nil {
			return result, err
		}
		cleared, err := clearReferences(ctx, tx, rules, item.id)
		if err == nil {
			_, err = tx.ExecContext(ctx, "DELETE FROM media WHERE id=?", item.id)
		}
		if err == nil {
			err = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
		if err != nil {
			return result, err
		}
		result.ReferencesCleared += cleared
		result.MediaRowsRemoved++
	}
	if options.RemoveUnused {
		for _, item := range records {
			if _, exists := objectMap[item.key]; !exists || item.created.After(cutoff) {
				continue
			}
			count, err := countReferences(ctx, service.db, rules, item.id)
			if err != nil {
				return result, err
			}
			if count != 0 {
				continue
			}
			result.UnusedMedia++
			result.MediaRowsRemoved++
			if options.DryRun {
				continue
			}
			if err := service.backend.Delete(ctx, item.key); err != nil {
				return result, err
			}
			if _, err := service.db.ExecContext(ctx, "DELETE FROM media WHERE id=?", item.id); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}

func countReferences(ctx context.Context, db *sql.DB, rules []ReferenceRule, mediaID uint64) (int64, error) {
	var total int64
	for _, rule := range rules {
		query := fmt.Sprintf("SELECT COUNT(*) FROM `%s` WHERE `%s`=?", rule.Table, rule.Column)
		var count int64
		if err := db.QueryRowContext(ctx, query, mediaID).Scan(&count); err != nil {
			return total, fmt.Errorf("check media reference %s: %w", rule.ID, err)
		}
		total += count
	}
	return total, nil
}

func (service *Service) selectedRules(ids []string) []ReferenceRule {
	if len(ids) == 0 {
		return nil
	}
	enabled := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		enabled[id] = struct{}{}
	}
	var result []ReferenceRule
	for _, rule := range service.cleanupRules {
		if _, ok := enabled[rule.ID]; ok {
			result = append(result, rule)
		}
	}
	return result
}

func clearReferences(ctx context.Context, tx *sql.Tx, rules []ReferenceRule, mediaID uint64) (int64, error) {
	var total int64
	for _, rule := range rules {
		query := fmt.Sprintf("UPDATE `%s` SET `%s`=NULL WHERE `%s`=?", rule.Table, rule.Column, rule.Column)
		result, err := tx.ExecContext(ctx, query, mediaID)
		if err != nil {
			return total, fmt.Errorf("clear media reference %s: %w", rule.ID, err)
		}
		count, _ := result.RowsAffected()
		total += count
	}
	return total, nil
}

func (service *Service) RunCleanup(ctx context.Context, checkInterval time.Duration) {
	if checkInterval <= 0 {
		checkInterval = time.Hour
	}
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			service.runScheduledCleanup(ctx)
		}
	}
}

func (service *Service) runScheduledCleanup(ctx context.Context) error {
	settings := map[string]string{}
	rows, err := service.db.QueryContext(ctx, "SELECT setting_key, setting_value FROM settings WHERE setting_key LIKE 'media.cleanup.%'")
	if err != nil {
		return err
	}
	for rows.Next() {
		var key, value string
		if rows.Scan(&key, &value) == nil {
			settings[key] = value
		}
	}
	_ = rows.Close()
	if settings["media.cleanup.enabled"] != "true" {
		return nil
	}
	interval, _ := strconv.Atoi(settings["media.cleanup.interval_hours"])
	lastUnix, _ := strconv.ParseInt(settings["media.cleanup.last_run"], 10, 64)
	if interval < 1 {
		interval = 24
	}
	if time.Since(time.Unix(lastUnix, 0)) < time.Duration(interval)*time.Hour {
		return nil
	}
	age, _ := strconv.Atoi(settings["media.cleanup.minimum_age_hours"])
	if age < 1 {
		age = 24
	}
	rules := strings.Split(settings["media.cleanup.rules"], ",")
	if _, err := service.Cleanup(ctx, CleanupOptions{MinimumAge: time.Duration(age) * time.Hour, RuleIDs: rules, RemoveUnused: settings["media.cleanup.remove_unused"] == "true"}); err != nil {
		return err
	}
	_, err = service.db.ExecContext(ctx, `INSERT INTO settings (setting_key, setting_value) VALUES ('media.cleanup.last_run', ?) ON DUPLICATE KEY UPDATE setting_value=VALUES(setting_value)`, strconv.FormatInt(time.Now().Unix(), 10))
	return err
}

func (service *Service) RunConfiguredCleanup(ctx context.Context) error {
	return service.runScheduledCleanup(ctx)
}
