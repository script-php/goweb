package auth

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// BootstrapAdministrator creates or updates the initial administrator and
// grants its role every permission currently registered by the application.
func BootstrapAdministrator(ctx context.Context, db *sql.DB, email, password string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return fmt.Errorf("a valid administrator email is required")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `INSERT INTO users (email, display_name, password_hash, active, password_changed_at)
		VALUES (?, 'Administrator', ?, 1, UTC_TIMESTAMP())
		ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id), password_hash = VALUES(password_hash), active = 1, password_changed_at = UTC_TIMESTAMP()`, email, hash)
	if err != nil {
		return fmt.Errorf("create administrator user: %w", err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, `INSERT INTO roles (name, slug, description)
		VALUES ('Administrator', 'administrator', 'Full application administration')
		ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id), name = VALUES(name), description = VALUES(description)`)
	if err != nil {
		return fmt.Errorf("create administrator role: %w", err)
	}
	roleID, err := result.LastInsertId()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT IGNORE INTO user_roles (user_id, role_id) VALUES (?, ?)", userID, roleID); err != nil {
		return fmt.Errorf("assign administrator role: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO role_permissions (role_id, permission_id) SELECT ?, id FROM permissions`, roleID); err != nil {
		return fmt.Errorf("grant administrator permissions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", userID); err != nil {
		return err
	}
	return tx.Commit()
}
