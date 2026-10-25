package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	appmail "script-php/goweb/internal/mail"
	"script-php/goweb/internal/ratelimit"
)

const SessionCookieName = "website_session"

var ErrInvalidCredentials = errors.New("invalid email or password")
var ErrTooManyAttempts = errors.New("too many login attempts")
var ErrTwoFactorRequired = errors.New("two-factor authentication required")
var ErrInvalidToken = errors.New("invalid or expired token")

type Service struct {
	db               *sql.DB
	sessionLength    time.Duration
	secureCookies    bool
	maxLoginAttempts int
	loginLockout     time.Duration
	limits           *ratelimit.Store
	mailer           appmail.Sender
	publicURL        string
	emailEnabled     bool
}

type ServiceOptions struct {
	SessionLength    time.Duration
	SecureCookies    bool
	MaxLoginAttempts int
	LoginLockout     time.Duration
	Mailer           appmail.Sender
	PublicURL        string
	EmailEnabled     bool
}

func NewService(db *sql.DB, options ServiceOptions) *Service {
	if options.SessionLength <= 0 {
		options.SessionLength = 24 * time.Hour
	}
	if options.MaxLoginAttempts <= 0 {
		options.MaxLoginAttempts = 5
	}
	if options.LoginLockout <= 0 {
		options.LoginLockout = 15 * time.Minute
	}
	return &Service{db: db, sessionLength: options.SessionLength, secureCookies: options.SecureCookies, maxLoginAttempts: options.MaxLoginAttempts, loginLockout: options.LoginLockout, limits: ratelimit.New(db), mailer: options.Mailer, publicURL: strings.TrimRight(options.PublicURL, "/"), emailEnabled: options.EmailEnabled}
}

func (service *Service) SecureCookies() bool { return service.secureCookies }

func (service *Service) LoginRetryAfterSeconds() int {
	seconds := int((service.loginLockout + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

func HashPassword(password string) (string, error) {
	if len(password) < 12 {
		return "", errors.New("password must contain at least 12 characters")
	}
	if len(password) > 72 {
		return "", errors.New("password must contain at most 72 bytes")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(hash), err
}

func CheckPassword(hash, password string) error {
	if hash == "" || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return ErrInvalidCredentials
	}
	return nil
}

func (service *Service) Login(ctx context.Context, email, password, ipAddress, userAgent string) (string, *User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	attemptKey := ipAddress + "|" + email
	blocked, err := service.loginBlocked(ctx, attemptKey, time.Now())
	if err != nil {
		return "", nil, fmt.Errorf("check login rate limit: %w", err)
	}
	if blocked {
		service.auditLogin(ctx, 0, email, "auth.login.blocked", ipAddress)
		return "", nil, ErrTooManyAttempts
	}
	var id uint64
	var hash string
	var active, twoFactor bool
	err = service.db.QueryRowContext(ctx, `SELECT id, COALESCE(password_hash, ''), active, two_factor_enabled FROM users WHERE email = ?`, email).Scan(&id, &hash, &active, &twoFactor)
	if errors.Is(err, sql.ErrNoRows) {
		// Equalize some work for unknown accounts.
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$12$C6UzMDM.H6dfI/f/IKxGhuW8dQ4q8zAqYhY5Z4Z6iGEXUXcQx5f.m"), []byte(password))
		loginErr := service.failedLogin(ctx, attemptKey, time.Now())
		service.auditLogin(ctx, 0, email, "auth.login.failed", ipAddress)
		return "", nil, loginErr
	}
	if err != nil {
		return "", nil, err
	}
	if !active || CheckPassword(hash, password) != nil {
		loginErr := service.failedLogin(ctx, attemptKey, time.Now())
		service.auditLogin(ctx, id, email, "auth.login.failed", ipAddress)
		return "", nil, loginErr
	}
	if err := service.clearLoginAttempts(ctx, attemptKey); err != nil {
		return "", nil, fmt.Errorf("clear login rate limit: %w", err)
	}
	if twoFactor {
		challenge, code, err := service.createCodeToken(ctx, id, "two_factor", 10*time.Minute)
		if err != nil {
			return "", nil, err
		}
		if err := service.send(ctx, email, "Your sign-in code", "Your sign-in code is: "+code+"\n\nIt expires in 10 minutes."); err != nil {
			return "", nil, err
		}
		return challenge, &User{ID: id, Email: email, TwoFactorEnabled: true}, ErrTwoFactorRequired
	}
	return service.createSession(ctx, id, email, ipAddress, userAgent)
}

func (service *Service) createSession(ctx context.Context, id uint64, email, ipAddress, userAgent string) (string, *User, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", nil, err
	}
	now := time.Now().UTC()
	_, err = service.db.ExecContext(ctx, `INSERT INTO sessions
		(token_hash, user_id, expires_at, last_used_at, ip_address, user_agent)
		VALUES (?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''))`, tokenHash(token), id, now.Add(service.sessionLength), now, limit(ipAddress, 45), limit(userAgent, 255))
	if err != nil {
		return "", nil, fmt.Errorf("create session: %w", err)
	}
	user, err := service.userBySessionHash(ctx, tokenHash(token))
	if err != nil {
		return "", nil, err
	}
	service.auditLogin(ctx, id, email, "auth.login.succeeded", ipAddress)
	service.SendSecurityAlert(ctx, id, "New sign-in", "A new sign-in to your account was completed from "+ipAddress+".")
	return token, user, nil
}

func (service *Service) VerifyTwoFactor(ctx context.Context, challenge, code, ipAddress, userAgent string) (string, *User, error) {
	userID, email, err := service.consumeToken(ctx, challenge, code, "two_factor", 5)
	if err != nil {
		return "", nil, err
	}
	return service.createSession(ctx, userID, email, ipAddress, userAgent)
}

func (service *Service) auditLogin(ctx context.Context, userID uint64, email, action, ipAddress string) {
	_, _ = service.db.ExecContext(ctx, `INSERT INTO audit_logs
		(actor_user_id, actor_email, action, target_type, target_id, ip_address)
		VALUES (NULLIF(?, 0), NULLIF(?, ''), ?, 'session', NULLIF(?, ''), NULLIF(?, ''))`,
		userID, limit(email, 255), action, email, limit(ipAddress, 45))
}

func (service *Service) loginBlocked(ctx context.Context, key string, now time.Time) (bool, error) {
	decision, err := service.limits.Check(ctx, "auth.login", key, now)
	return !decision.Allowed, err
}

func (service *Service) failedLogin(ctx context.Context, key string, now time.Time) error {
	decision, err := service.limits.Hit(ctx, "auth.login", key, service.maxLoginAttempts, service.loginLockout, now)
	if err != nil {
		return fmt.Errorf("record login rate limit: %w", err)
	}
	if !decision.Allowed {
		return ErrTooManyAttempts
	}
	return ErrInvalidCredentials
}

func (service *Service) clearLoginAttempts(ctx context.Context, key string) error {
	return service.limits.Reset(ctx, "auth.login", key)
}

func (service *Service) ResolveSession(ctx context.Context, token string) (*User, error) {
	if len(token) < 32 {
		return nil, nil
	}
	hash := tokenHash(token)
	user, err := service.userBySessionHash(ctx, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_, _ = service.db.ExecContext(ctx, "UPDATE sessions SET last_used_at = UTC_TIMESTAMP() WHERE token_hash = ?", hash)
	return user, nil
}

func (service *Service) userBySessionHash(ctx context.Context, hash string) (*User, error) {
	var user User
	var roles, permissions string
	err := service.db.QueryRowContext(ctx, `
		SELECT u.id, u.email, COALESCE(u.display_name, ''), COALESCE(u.avatar_media_id, 0), u.force_password_change,
		       u.email_verified_at IS NOT NULL, u.two_factor_enabled,
		       COALESCE(GROUP_CONCAT(DISTINCT r.slug ORDER BY r.slug), ''),
		       COALESCE(GROUP_CONCAT(DISTINCT p.name ORDER BY p.name), '')
		FROM sessions s
		JOIN users u ON u.id = s.user_id AND u.active = 1
		LEFT JOIN user_roles ur ON ur.user_id = u.id
		LEFT JOIN roles r ON r.id = ur.role_id
		LEFT JOIN role_permissions rp ON rp.role_id = r.id
		LEFT JOIN permissions p ON p.id = rp.permission_id
		WHERE s.token_hash = ? AND s.expires_at > UTC_TIMESTAMP()
		GROUP BY u.id, u.email, u.display_name, u.avatar_media_id`, hash).
		Scan(&user.ID, &user.Email, &user.DisplayName, &user.AvatarMediaID, &user.ForcePasswordChange, &user.EmailVerified, &user.TwoFactorEnabled, &roles, &permissions)
	if err != nil {
		return nil, err
	}
	if roles != "" {
		user.Roles = strings.Split(roles, ",")
	}
	if permissions != "" {
		user.Permissions = strings.Split(permissions, ",")
	}
	return &user, nil
}

func (service *Service) RequestPasswordReset(ctx context.Context, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	var id uint64
	if err := service.db.QueryRowContext(ctx, "SELECT id FROM users WHERE email = ? AND active = 1", email).Scan(&id); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	token, err := service.createLinkToken(ctx, id, "password_reset", time.Hour)
	if err != nil {
		return err
	}
	return service.send(ctx, email, "Reset your password", "Use this link to reset your password:\n\n"+service.publicURL+"/reset-password?token="+token+"\n\nIt expires in one hour.")
}

func (service *Service) ResetPassword(ctx context.Context, token, password string) error {
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	userID, _, err := service.consumeToken(ctx, token, "", "password_reset", 1)
	if err != nil {
		return err
	}
	tx, err := service.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE users SET password_hash = ?, password_changed_at = UTC_TIMESTAMP(), force_password_change = 0 WHERE id = ?", hash, userID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (service *Service) SendVerification(ctx context.Context, userID uint64, email string) error {
	token, err := service.createLinkToken(ctx, userID, "email_verification", 24*time.Hour)
	if err != nil {
		return err
	}
	return service.send(ctx, email, "Verify your email address", "Verify your email address using this link:\n\n"+service.publicURL+"/verify-email?token="+token+"\n\nIt expires in 24 hours.")
}

func (service *Service) VerifyEmail(ctx context.Context, token string) error {
	userID, _, err := service.consumeToken(ctx, token, "", "email_verification", 1)
	if err != nil {
		return err
	}
	_, err = service.db.ExecContext(ctx, "UPDATE users SET email_verified_at = UTC_TIMESTAMP() WHERE id = ?", userID)
	return err
}

func (service *Service) SetTwoFactor(ctx context.Context, userID uint64, enabled bool) error {
	_, err := service.db.ExecContext(ctx, "UPDATE users SET two_factor_enabled = ? WHERE id = ?", enabled, userID)
	return err
}

func (service *Service) SendSecurityAlert(ctx context.Context, userID uint64, subject, text string) {
	if service.mailer == nil {
		return
	}
	var enabled string
	if err := service.db.QueryRowContext(ctx, "SELECT setting_value FROM settings WHERE setting_key='security.alerts'").Scan(&enabled); err != nil || enabled != "true" {
		return
	}
	var email string
	if err := service.db.QueryRowContext(ctx, "SELECT email FROM users WHERE id = ?", userID).Scan(&email); err == nil {
		_ = service.send(ctx, email, subject, text)
	}
}

func (service *Service) FeatureEnabled(ctx context.Context, key string, fallback bool) bool {
	var value string
	if err := service.db.QueryRowContext(ctx, "SELECT setting_value FROM settings WHERE setting_key = ?", key).Scan(&value); err != nil {
		return fallback
	}
	return value == "true"
}

func (service *Service) EmailAvailable(ctx context.Context) bool {
	return service.mailer != nil && service.FeatureEnabled(ctx, "mail.enabled", service.emailEnabled)
}

func (service *Service) send(ctx context.Context, email, subject, text string) error {
	if service.mailer == nil {
		return errors.New("email delivery is not configured")
	}
	return service.mailer.Send(ctx, appmail.Message{To: email, Subject: subject, Text: text})
}

func (service *Service) createLinkToken(ctx context.Context, userID uint64, purpose string, lifetime time.Duration) (string, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", err
	}
	_, err = service.db.ExecContext(ctx, `DELETE FROM auth_tokens WHERE user_id = ? AND purpose = ?`, userID, purpose)
	if err != nil {
		return "", err
	}
	_, err = service.db.ExecContext(ctx, `INSERT INTO auth_tokens (selector_hash, user_id, purpose, expires_at) VALUES (?, ?, ?, ?)`, tokenHash(token), userID, purpose, time.Now().UTC().Add(lifetime))
	return token, err
}

func (service *Service) createCodeToken(ctx context.Context, userID uint64, purpose string, lifetime time.Duration) (string, string, error) {
	selector, err := randomToken(32)
	if err != nil {
		return "", "", err
	}
	raw, err := randomToken(4)
	if err != nil {
		return "", "", err
	}
	var number uint64
	_, _ = fmt.Sscanf(raw[:8], "%x", &number)
	code := fmt.Sprintf("%06d", number%1000000)
	_, err = service.db.ExecContext(ctx, `INSERT INTO auth_tokens (selector_hash, secret_hash, user_id, purpose, expires_at) VALUES (?, ?, ?, ?, ?)`, tokenHash(selector), tokenHash(code), userID, purpose, time.Now().UTC().Add(lifetime))
	return selector, code, err
}

func (service *Service) consumeToken(ctx context.Context, selector, secret, purpose string, maxAttempts int) (uint64, string, error) {
	tx, err := service.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback()
	var userID uint64
	var email, secretHash string
	var attempts int
	err = tx.QueryRowContext(ctx, `SELECT t.user_id, u.email, COALESCE(t.secret_hash, ''), t.attempts FROM auth_tokens t JOIN users u ON u.id=t.user_id WHERE t.selector_hash=? AND t.purpose=? AND t.expires_at>UTC_TIMESTAMP() FOR UPDATE`, tokenHash(selector), purpose).Scan(&userID, &email, &secretHash, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", ErrInvalidToken
	}
	if err != nil {
		return 0, "", err
	}
	if secretHash != "" && tokenHash(strings.TrimSpace(secret)) != secretHash {
		attempts++
		if attempts >= maxAttempts {
			_, _ = tx.ExecContext(ctx, "DELETE FROM auth_tokens WHERE selector_hash=?", tokenHash(selector))
		} else {
			_, _ = tx.ExecContext(ctx, "UPDATE auth_tokens SET attempts=? WHERE selector_hash=?", attempts, tokenHash(selector))
		}
		_ = tx.Commit()
		return 0, "", ErrInvalidToken
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM auth_tokens WHERE selector_hash=?", tokenHash(selector)); err != nil {
		return 0, "", err
	}
	if err = tx.Commit(); err != nil {
		return 0, "", err
	}
	return userID, email, nil
}

func (service *Service) ChangePassword(ctx context.Context, userID uint64, currentToken, password string) error {
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	tx, err := service.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ?, password_changed_at = UTC_TIMESTAMP(), force_password_change = 0 WHERE id = ?`, hash, userID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?", userID, tokenHash(currentToken)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs (actor_user_id, action, target_type, target_id)
		VALUES (?, 'user.password_changed', 'user', ?)`, userID, fmt.Sprint(userID)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	service.SendSecurityAlert(ctx, userID, "Password changed", "Your account password was changed. If this was not you, contact the site administrator immediately.")
	return nil
}

func (service *Service) Logout(ctx context.Context, token, ipAddress string) error {
	if token == "" {
		return nil
	}
	tx, err := service.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var userID uint64
	var email string
	_ = tx.QueryRowContext(ctx, `SELECT u.id, u.email FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.token_hash = ?`, tokenHash(token)).Scan(&userID, &email)
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", tokenHash(token)); err != nil {
		return err
	}
	if userID != 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs
			(actor_user_id, actor_email, action, target_type, target_id, ip_address)
			VALUES (?, ?, 'auth.logout', 'session', ?, NULLIF(?, ''))`, userID, email, email, limit(ipAddress, 45)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (service *Service) SetSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookieName, Value: token, Path: "/", MaxAge: int(service.sessionLength.Seconds()), HttpOnly: true, Secure: service.secureCookies, SameSite: http.SameSiteLaxMode})
}

func (service *Service) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: service.secureCookies, SameSite: http.SameSiteLaxMode})
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func limit(value string, maximum int) string {
	if len(value) > maximum {
		return value[:maximum]
	}
	return value
}
