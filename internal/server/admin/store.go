package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type User struct {
	ID                  uint64
	Email               string
	DisplayName         string
	Active              bool
	ForcePasswordChange bool
	Roles               []uint64
	RoleNames           []string
	PasswordHash        string
	AvatarMediaID       uint64
	CreatedAt           time.Time
}

type UserListOptions struct {
	Query  string
	Limit  int
	Offset int
}

type Role struct {
	ID          uint64
	Name        string
	Slug        string
	Description string
	Permissions []string
	CreatedAt   time.Time
	UserCount   int
}

type AuditEntry struct {
	ID         uint64
	ActorEmail string
	Action     string
	TargetType string
	TargetID   string
	Details    string
	IPAddress  string
	CreatedAt  time.Time
}

type AuditListOptions struct {
	Query  string
	Action string
	From   string
	To     string
	Limit  int
	Offset int
}

type EmailDelivery struct {
	ID                                    uint64
	Recipient, Subject, Status, LastError string
	Attempts                              int
	CreatedAt                             time.Time
	NextAttemptAt, SentAt                 sql.NullTime
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (store *Store) EmailDeliveries(ctx context.Context, limit int) ([]EmailDelivery, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := store.db.QueryContext(ctx, `SELECT id, recipient, subject, status, attempts,
		COALESCE(last_error, ''), next_attempt_at, sent_at, created_at FROM email_queue ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var deliveries []EmailDelivery
	for rows.Next() {
		var item EmailDelivery
		if err := rows.Scan(&item.ID, &item.Recipient, &item.Subject, &item.Status, &item.Attempts, &item.LastError, &item.NextAttemptAt, &item.SentAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		deliveries = append(deliveries, item)
	}
	return deliveries, rows.Err()
}

func (store *Store) RetryEmail(ctx context.Context, id uint64) error {
	result, err := store.db.ExecContext(ctx, `UPDATE email_queue SET status='queued', attempts=0, next_attempt_at=UTC_TIMESTAMP(), last_error=NULL WHERE id=? AND status='failed'`, id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (store *Store) EnqueueEmail(ctx context.Context, recipient, subject, body string) error {
	_, err := store.db.ExecContext(ctx, `INSERT INTO email_queue (recipient, subject, body, status, next_attempt_at) VALUES (?, ?, ?, 'queued', UTC_TIMESTAMP())`, recipient, subject, body)
	return err
}

func (store *Store) Counts(ctx context.Context) (users, roles int, err error) {
	if err = store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&users); err != nil {
		return 0, 0, fmt.Errorf("count users: %w", err)
	}
	if err = store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM roles").Scan(&roles); err != nil {
		return 0, 0, fmt.Errorf("count roles: %w", err)
	}
	return users, roles, nil
}

func (store *Store) Users(ctx context.Context) ([]User, error) {
	users, _, err := store.ListUsers(ctx, UserListOptions{Limit: 1000})
	return users, err
}

func (store *Store) ListUsers(ctx context.Context, options UserListOptions) ([]User, int, error) {
	query := strings.TrimSpace(options.Query)
	if options.Limit < 1 || options.Limit > 100 {
		options.Limit = 20
	}
	if options.Offset < 0 {
		options.Offset = 0
	}
	var total int
	if err := store.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM users u
		WHERE (? = '' OR u.email LIKE CONCAT('%', ?, '%') OR COALESCE(u.display_name, '') LIKE CONCAT('%', ?, '%'))`,
		query, query, query).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count filtered users: %w", err)
	}
	rows, err := store.db.QueryContext(ctx, `
		SELECT u.id, u.email, COALESCE(u.display_name, ''), u.active, u.force_password_change, COALESCE(u.avatar_media_id, 0), u.created_at,
		       COALESCE(GROUP_CONCAT(DISTINCT ur.role_id ORDER BY ur.role_id), ''),
		       COALESCE(GROUP_CONCAT(DISTINCT r.name ORDER BY r.name SEPARATOR '|'), '')
		FROM users u
		LEFT JOIN user_roles ur ON ur.user_id = u.id
		LEFT JOIN roles r ON r.id = ur.role_id
		WHERE (? = '' OR u.email LIKE CONCAT('%', ?, '%') OR COALESCE(u.display_name, '') LIKE CONCAT('%', ?, '%'))
		GROUP BY u.id, u.email, u.display_name, u.active, u.force_password_change, u.avatar_media_id, u.created_at
		ORDER BY u.id DESC
		LIMIT ? OFFSET ?`, query, query, query, options.Limit, options.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var user User
		var roles, roleNames string
		if err := rows.Scan(&user.ID, &user.Email, &user.DisplayName, &user.Active, &user.ForcePasswordChange, &user.AvatarMediaID, &user.CreatedAt, &roles, &roleNames); err != nil {
			return nil, 0, fmt.Errorf("scan user: %w", err)
		}
		user.Roles = parseIDs(roles)
		if roleNames != "" {
			user.RoleNames = strings.Split(roleNames, "|")
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

func (store *Store) User(ctx context.Context, id uint64) (User, error) {
	var user User
	var roles string
	err := store.db.QueryRowContext(ctx, `
		SELECT u.id, u.email, COALESCE(u.display_name, ''), u.active, u.force_password_change, COALESCE(u.avatar_media_id, 0), u.created_at,
		       COALESCE(GROUP_CONCAT(ur.role_id ORDER BY ur.role_id), '')
		FROM users u LEFT JOIN user_roles ur ON ur.user_id = u.id
		WHERE u.id = ? GROUP BY u.id, u.email, u.display_name, u.active, u.force_password_change, u.avatar_media_id, u.created_at`, id).
		Scan(&user.ID, &user.Email, &user.DisplayName, &user.Active, &user.ForcePasswordChange, &user.AvatarMediaID, &user.CreatedAt, &roles)
	if err != nil {
		return User{}, err
	}
	user.Roles = parseIDs(roles)
	return user, nil
}

func (store *Store) SaveUser(ctx context.Context, user User) (uint64, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if user.ID == 0 {
		result, err := tx.ExecContext(ctx, "INSERT INTO users (email, display_name, password_hash, active, force_password_change, avatar_media_id, password_changed_at) VALUES (?, NULLIF(?, ''), ?, ?, ?, NULLIF(?, 0), UTC_TIMESTAMP())", user.Email, user.DisplayName, user.PasswordHash, user.Active, user.ForcePasswordChange, user.AvatarMediaID)
		if err != nil {
			return 0, fmt.Errorf("create user: %w", err)
		}
		inserted, err := result.LastInsertId()
		if err != nil {
			return 0, err
		}
		user.ID = uint64(inserted)
	} else {
		if user.PasswordHash == "" {
			_, err = tx.ExecContext(ctx, "UPDATE users SET email = ?, display_name = NULLIF(?, ''), active = ?, force_password_change = ? WHERE id = ?", user.Email, user.DisplayName, user.Active, user.ForcePasswordChange, user.ID)
		} else {
			_, err = tx.ExecContext(ctx, "UPDATE users SET email = ?, display_name = NULLIF(?, ''), password_hash = ?, password_changed_at = UTC_TIMESTAMP(), active = ?, force_password_change = ? WHERE id = ?", user.Email, user.DisplayName, user.PasswordHash, user.Active, user.ForcePasswordChange, user.ID)
		}
		if err != nil {
			return 0, fmt.Errorf("update user: %w", err)
		}
		if user.PasswordHash != "" {
			if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", user.ID); err != nil {
				return 0, fmt.Errorf("revoke user sessions: %w", err)
			}
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM user_roles WHERE user_id = ?", user.ID); err != nil {
		return 0, err
	}
	for _, roleID := range user.Roles {
		if _, err := tx.ExecContext(ctx, "INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)", user.ID, roleID); err != nil {
			return 0, fmt.Errorf("assign user role: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return user.ID, nil
}

func (store *Store) SetUserAvatar(ctx context.Context, userID, mediaID uint64) error {
	result, err := store.db.ExecContext(ctx, "UPDATE users SET avatar_media_id=NULLIF(?, 0) WHERE id=?", mediaID, userID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (store *Store) DeleteUser(ctx context.Context, id uint64) error {
	_, err := store.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", id)
	return err
}

func (store *Store) RevokeUserSessions(ctx context.Context, id uint64) (int64, error) {
	result, err := store.db.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", id)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (store *Store) UserHasRole(ctx context.Context, userID uint64, slug string) (bool, error) {
	var count int
	err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_roles ur JOIN roles r ON r.id = ur.role_id WHERE ur.user_id = ? AND r.slug = ?`, userID, slug).Scan(&count)
	return count > 0, err
}

func (store *Store) UsersWithRole(ctx context.Context, slug string) (int, error) {
	var count int
	err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_roles ur JOIN roles r ON r.id = ur.role_id JOIN users u ON u.id = ur.user_id AND u.active = 1 WHERE r.slug = ?`, slug).Scan(&count)
	return count, err
}

func (store *Store) Roles(ctx context.Context) ([]Role, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT r.id, r.name, r.slug, COALESCE(r.description, ''), r.created_at,
		       COALESCE(GROUP_CONCAT(DISTINCT p.name ORDER BY p.name), ''), COUNT(DISTINCT ur.user_id)
		FROM roles r
		LEFT JOIN role_permissions rp ON rp.role_id = r.id
		LEFT JOIN permissions p ON p.id = rp.permission_id
		LEFT JOIN user_roles ur ON ur.role_id = r.id
		GROUP BY r.id, r.name, r.slug, r.description, r.created_at
		ORDER BY r.name`)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()
	var roles []Role
	for rows.Next() {
		var role Role
		var permissions string
		if err := rows.Scan(&role.ID, &role.Name, &role.Slug, &role.Description, &role.CreatedAt, &permissions, &role.UserCount); err != nil {
			return nil, err
		}
		if permissions != "" {
			role.Permissions = strings.Split(permissions, ",")
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

func (store *Store) Role(ctx context.Context, id uint64) (Role, error) {
	var role Role
	var permissions string
	err := store.db.QueryRowContext(ctx, `
		SELECT r.id, r.name, r.slug, COALESCE(r.description, ''), r.created_at,
		       COALESCE(GROUP_CONCAT(DISTINCT p.name ORDER BY p.name), ''), COUNT(DISTINCT ur.user_id)
		FROM roles r
		LEFT JOIN role_permissions rp ON rp.role_id = r.id
		LEFT JOIN permissions p ON p.id = rp.permission_id
		LEFT JOIN user_roles ur ON ur.role_id = r.id
		WHERE r.id = ? GROUP BY r.id, r.name, r.slug, r.description, r.created_at`, id).
		Scan(&role.ID, &role.Name, &role.Slug, &role.Description, &role.CreatedAt, &permissions, &role.UserCount)
	if permissions != "" {
		role.Permissions = strings.Split(permissions, ",")
	}
	return role, err
}

func (store *Store) SaveRole(ctx context.Context, role Role) (uint64, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if role.ID == 0 {
		result, err := tx.ExecContext(ctx, "INSERT INTO roles (name, slug, description) VALUES (?, ?, NULLIF(?, ''))", role.Name, role.Slug, role.Description)
		if err != nil {
			return 0, fmt.Errorf("create role: %w", err)
		}
		inserted, err := result.LastInsertId()
		if err != nil {
			return 0, err
		}
		role.ID = uint64(inserted)
	} else if _, err := tx.ExecContext(ctx, "UPDATE roles SET name = ?, slug = ?, description = NULLIF(?, '') WHERE id = ?", role.Name, role.Slug, role.Description, role.ID); err != nil {
		return 0, fmt.Errorf("update role: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM role_permissions WHERE role_id = ?", role.ID); err != nil {
		return 0, err
	}
	for _, permission := range role.Permissions {
		if _, err := tx.ExecContext(ctx, `INSERT INTO role_permissions (role_id, permission_id)
			SELECT ?, id FROM permissions WHERE name = ?`, role.ID, permission); err != nil {
			return 0, fmt.Errorf("assign role permission: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return role.ID, nil
}

func (store *Store) DeleteRole(ctx context.Context, id uint64) error {
	_, err := store.db.ExecContext(ctx, "DELETE FROM roles WHERE id = ?", id)
	return err
}

func (store *Store) SyncPermissions(ctx context.Context, permissions []string) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, permission := range permissions {
		if _, err := tx.ExecContext(ctx, `INSERT INTO permissions (name) VALUES (?) ON DUPLICATE KEY UPDATE name = VALUES(name)`, permission); err != nil {
			return fmt.Errorf("sync permission %s: %w", permission, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO role_permissions (role_id, permission_id)
		SELECT r.id, p.id FROM roles r CROSS JOIN permissions p WHERE r.slug = 'administrator'`); err != nil {
		return fmt.Errorf("grant administrator permissions: %w", err)
	}
	return tx.Commit()
}

func (store *Store) AddAudit(ctx context.Context, actorID uint64, actorEmail, action, targetType, targetID, details, ipAddress string) error {
	_, err := store.db.ExecContext(ctx, `INSERT INTO audit_logs
		(actor_user_id, actor_email, action, target_type, target_id, details, ip_address)
		VALUES (NULLIF(?, 0), NULLIF(?, ''), ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''))`,
		actorID, actorEmail, action, targetType, targetID, details, ipAddress)
	return err
}

func (store *Store) AuditEntries(ctx context.Context, limit int) ([]AuditEntry, error) {
	entries, _, err := store.ListAuditEntries(ctx, AuditListOptions{Limit: limit})
	return entries, err
}

func (store *Store) ListAuditEntries(ctx context.Context, options AuditListOptions) ([]AuditEntry, int, error) {
	if options.Limit < 1 || options.Limit > 5000 {
		options.Limit = 50
	}
	if options.Offset < 0 {
		options.Offset = 0
	}
	options.Query = strings.TrimSpace(options.Query)
	options.Action = strings.TrimSpace(options.Action)
	where := `(? = '' OR actor_email LIKE CONCAT('%', ?, '%') OR action LIKE CONCAT('%', ?, '%') OR target_type LIKE CONCAT('%', ?, '%') OR target_id LIKE CONCAT('%', ?, '%') OR details LIKE CONCAT('%', ?, '%') OR ip_address LIKE CONCAT('%', ?, '%')) AND (? = '' OR action = ?) AND (? = '' OR created_at >= ?) AND (? = '' OR created_at < DATE_ADD(?, INTERVAL 1 DAY))`
	arguments := []any{options.Query, options.Query, options.Query, options.Query, options.Query, options.Query, options.Query, options.Action, options.Action, options.From, options.From, options.To, options.To}
	var total int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_logs WHERE "+where, arguments...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := store.db.QueryContext(ctx, `SELECT id, COALESCE(actor_email, ''), action, target_type,
		COALESCE(target_id, ''), COALESCE(details, ''), COALESCE(ip_address, ''), created_at
		FROM audit_logs WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, append(arguments, options.Limit, options.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var entries []AuditEntry
	for rows.Next() {
		var entry AuditEntry
		if err := rows.Scan(&entry.ID, &entry.ActorEmail, &entry.Action, &entry.TargetType, &entry.TargetID, &entry.Details, &entry.IPAddress, &entry.CreatedAt); err != nil {
			return nil, 0, err
		}
		entries = append(entries, entry)
	}
	return entries, total, rows.Err()
}

func (store *Store) DeleteAuditBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := store.db.ExecContext(ctx, "DELETE FROM audit_logs WHERE created_at < ?", cutoff.UTC())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (store *Store) AuditActions(ctx context.Context) ([]string, error) {
	rows, err := store.db.QueryContext(ctx, "SELECT DISTINCT action FROM audit_logs ORDER BY action")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			return nil, err
		}
		actions = append(actions, action)
	}
	return actions, rows.Err()
}

func (store *Store) Settings(ctx context.Context) (map[string]string, error) {
	rows, err := store.db.QueryContext(ctx, "SELECT setting_key, setting_value FROM settings ORDER BY setting_key")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	settings := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		settings[key] = value
	}
	return settings, rows.Err()
}

func (store *Store) SaveSettings(ctx context.Context, settings map[string]string) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, value := range settings {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (setting_key, setting_value) VALUES (?, ?)
			ON DUPLICATE KEY UPDATE setting_value = VALUES(setting_value)`, key, value); err != nil {
			return fmt.Errorf("save setting %s: %w", key, err)
		}
	}
	return tx.Commit()
}

func parseIDs(value string) []uint64 {
	if value == "" {
		return nil
	}
	var ids []uint64
	for _, part := range strings.Split(value, ",") {
		var id uint64
		if _, err := fmt.Sscan(part, &id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func IsNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }
