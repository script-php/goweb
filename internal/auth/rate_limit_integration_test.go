//go:build integration

package auth

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func TestLoginRateLimitIntegration(t *testing.T) {
	dsn := os.Getenv("WEBSITE_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("WEBSITE_INTEGRATION_DSN is not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	password := "Integration!2345"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	email := "rate-" + strings.ReplaceAll(time.Now().UTC().Format("20060102150405.000000000"), ".", "") + "@example.invalid"
	result, err := db.ExecContext(ctx, `INSERT INTO users (email, display_name, password_hash, active, password_changed_at) VALUES (?, 'Rate limit test', ?, 1, UTC_TIMESTAMP())`, email, hash)
	if err != nil {
		t.Fatal(err)
	}
	userID, _ := result.LastInsertId()
	t.Cleanup(func() { _, _ = db.Exec("DELETE FROM users WHERE id = ?", userID) })

	t.Run("account and IP keys are isolated", func(t *testing.T) {
		service := NewService(db, ServiceOptions{MaxLoginAttempts: 2, LoginLockout: 2 * time.Second})
		if _, _, err := service.Login(ctx, email, "wrong password", "192.0.2.1", "integration"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("first failure = %v", err)
		}
		if _, _, err := service.Login(ctx, email, "wrong password", "192.0.2.1", "integration"); !errors.Is(err, ErrTooManyAttempts) {
			t.Fatalf("lockout failure = %v", err)
		}
		if _, _, err := service.Login(ctx, email, password, "192.0.2.1", "integration"); !errors.Is(err, ErrTooManyAttempts) {
			t.Fatalf("valid password bypassed lockout: %v", err)
		}
		if _, _, err := service.Login(ctx, email, password, "192.0.2.2", "integration"); err != nil {
			t.Fatalf("different IP was blocked: %v", err)
		}
		if _, _, err := service.Login(ctx, "another-"+email, "wrong password", "192.0.2.1", "integration"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("different account shared lockout: %v", err)
		}
	})

	t.Run("success clears failures", func(t *testing.T) {
		service := NewService(db, ServiceOptions{MaxLoginAttempts: 2, LoginLockout: time.Minute})
		if _, _, err := service.Login(ctx, email, "wrong password", "192.0.2.3", "integration"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatal(err)
		}
		if _, _, err := service.Login(ctx, email, password, "192.0.2.3", "integration"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := service.Login(ctx, email, "wrong password", "192.0.2.3", "integration"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("failure count was not cleared: %v", err)
		}
	})

	t.Run("lockout expires", func(t *testing.T) {
		service := NewService(db, ServiceOptions{MaxLoginAttempts: 1, LoginLockout: time.Second})
		if _, _, err := service.Login(ctx, email, "wrong password", "192.0.2.4", "integration"); !errors.Is(err, ErrTooManyAttempts) {
			t.Fatal(err)
		}
		if _, _, err := service.Login(ctx, email, password, "192.0.2.4", "integration"); !errors.Is(err, ErrTooManyAttempts) {
			t.Fatalf("lockout did not reject valid credentials: %v", err)
		}
		time.Sleep(1100 * time.Millisecond)
		if _, _, err := service.Login(ctx, email, password, "192.0.2.4", "integration"); err != nil {
			t.Fatalf("login remained blocked after expiry: %v", err)
		}
	})

	t.Run("concurrent failures produce a lockout", func(t *testing.T) {
		service := NewService(db, ServiceOptions{MaxLoginAttempts: 2, LoginLockout: time.Minute})
		var wait sync.WaitGroup
		errorsSeen := make(chan error, 4)
		for index := 0; index < 4; index++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				_, _, loginErr := service.Login(context.Background(), email, "wrong password", "192.0.2.5", "integration")
				errorsSeen <- loginErr
			}()
		}
		wait.Wait()
		close(errorsSeen)
		blocked := false
		for loginErr := range errorsSeen {
			blocked = blocked || errors.Is(loginErr, ErrTooManyAttempts)
		}
		if !blocked {
			t.Fatal("concurrent failures did not reach the lockout threshold")
		}
		if _, _, err := service.Login(ctx, email, password, "192.0.2.5", "integration"); !errors.Is(err, ErrTooManyAttempts) {
			t.Fatalf("concurrent lockout did not persist: %v", err)
		}
	})
}
