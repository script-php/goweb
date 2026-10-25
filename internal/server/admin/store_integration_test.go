//go:build integration

package admin

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func TestUserRolePermissionStoreIntegration(t *testing.T) {
	dsn := os.Getenv("WEBSITE_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("WEBSITE_INTEGRATION_DSN is not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	store := NewStore(db)
	suffix := time.Now().UTC().Format("20060102150405.000000000")
	role := Role{Name: "Integration " + suffix, Slug: "integration-" + strings.ReplaceAll(suffix, ".", ""), Permissions: []string{"admin.dashboard.view"}}
	roleID, err := store.SaveRole(ctx, role)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.DeleteRole(context.Background(), roleID) })

	email := "integration-" + strings.ReplaceAll(suffix, ".", "") + "@example.invalid"
	userID, err := store.SaveUser(ctx, User{Email: email, DisplayName: "Integration Test", Active: true, Roles: []uint64{roleID}, PasswordHash: "integration-only"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.DeleteUser(context.Background(), userID) })

	user, err := store.User(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != email || len(user.Roles) != 1 || user.Roles[0] != roleID {
		t.Fatalf("unexpected user: %#v", user)
	}
	users, total, err := store.ListUsers(ctx, UserListOptions{Query: email, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(users) != 1 || len(users[0].RoleNames) != 1 {
		t.Fatalf("unexpected search result: total=%d users=%#v", total, users)
	}
	savedRole, err := store.Role(ctx, roleID)
	if err != nil {
		t.Fatal(err)
	}
	if len(savedRole.Permissions) != 1 || savedRole.Permissions[0] != "admin.dashboard.view" || savedRole.UserCount != 1 {
		t.Fatalf("unexpected role: %#v", savedRole)
	}
	if count, err := store.RevokeUserSessions(ctx, userID); err != nil || count != 0 {
		t.Fatalf("RevokeUserSessions() = %d, %v", count, err)
	}

	rateScope := "integration." + strings.ReplaceAll(suffix, ".", "")
	rateHash := strings.Repeat("a", 64)
	if _, err := db.ExecContext(ctx, `INSERT INTO rate_limits (scope,key_hash,hits,window_started_at,blocked_until,limited_count,last_limited_at) VALUES (?,?,3,UTC_TIMESTAMP(),DATE_ADD(UTC_TIMESTAMP(), INTERVAL 1 MINUTE),1,UTC_TIMESTAMP())`, rateScope, rateHash); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec("DELETE FROM rate_limits WHERE scope=? AND key_hash=?", rateScope, rateHash) })
	limits, err := store.RateLimits(ctx, rateScope, 10)
	if err != nil || len(limits) != 1 || limits[0].Hits != 3 || limits[0].LimitedCount != 1 {
		t.Fatalf("RateLimits() = %#v, %v", limits, err)
	}
	if err := store.ClearRateLimit(ctx, rateScope, rateHash); err != nil {
		t.Fatal(err)
	}
	if limits, err = store.RateLimits(ctx, rateScope, 10); err != nil || len(limits) != 0 {
		t.Fatalf("cleared RateLimits() = %#v, %v", limits, err)
	}
}
