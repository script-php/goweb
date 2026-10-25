package admin

import (
	"net/http"

	"script-php/goweb/internal/server/catalog"
)

type builtinModule struct{}

func (builtinModule) Register(admin *Admin) error {
	routes := []struct {
		pattern string
		page    catalog.Page
		handler http.HandlerFunc
	}{
		{"GET /admin/{$}", catalog.Page{ID: "dashboard", Label: "Dashboard", Path: "/admin/", Description: "Overview of the application", Permission: "admin.dashboard.view", Navigation: true, Order: 10}, admin.dashboard},
		{"GET /admin/users", catalog.Page{ID: "users", Label: "Users", Path: "/admin/users", Description: "Create, edit, and remove users", Permission: "admin.users.view", Navigation: true, Order: 20}, admin.users},
		{"GET /admin/users/new", catalog.Page{ID: "users.new", Label: "Create user", Path: "/admin/users/new", Permission: "admin.users.manage"}, admin.userNew},
		{"POST /admin/users", catalog.Page{ID: "users.create", Label: "Create user action", Path: "/admin/users", Permission: "admin.users.manage"}, admin.userCreate},
		{"GET /admin/users/{id}", catalog.Page{ID: "users.edit", Label: "Edit user", Path: "/admin/users/{id}", Permission: "admin.users.manage"}, admin.userEdit},
		{"POST /admin/users/{id}", catalog.Page{ID: "users.update", Label: "Update user action", Path: "/admin/users/{id}", Permission: "admin.users.manage"}, admin.userUpdate},
		{"POST /admin/users/{id}/delete", catalog.Page{ID: "users.delete", Label: "Delete user action", Path: "/admin/users/{id}/delete", Permission: "admin.users.manage"}, admin.userDelete},
		{"POST /admin/users/{id}/revoke-sessions", catalog.Page{ID: "users.revoke-sessions", Label: "Revoke user sessions", Path: "/admin/users/{id}/revoke-sessions", Permission: "admin.users.manage"}, admin.userRevokeSessions},
		{"GET /admin/users/{id}/avatar", catalog.Page{ID: "users.avatar", Label: "User avatar", Path: "/admin/users/{id}/avatar", Permission: "admin.users.manage"}, admin.userAvatar},
		{"POST /admin/users/{id}/avatar", catalog.Page{ID: "users.avatar.update", Label: "Change user avatar", Path: "/admin/users/{id}/avatar", Permission: "admin.users.manage"}, admin.userAvatarUpdate},
		{"POST /admin/users/{id}/avatar/remove", catalog.Page{ID: "users.avatar.remove", Label: "Remove user avatar", Path: "/admin/users/{id}/avatar/remove", Permission: "admin.users.manage"}, admin.userAvatarRemove},
		{"GET /admin/roles", catalog.Page{ID: "roles", Label: "Roles", Path: "/admin/roles", Description: "Manage roles and permissions", Permission: "admin.roles.view", Navigation: true, Order: 30}, admin.roles},
		{"GET /admin/roles/new", catalog.Page{ID: "roles.new", Label: "Create role", Path: "/admin/roles/new", Permission: "admin.roles.manage"}, admin.roleNew},
		{"POST /admin/roles", catalog.Page{ID: "roles.create", Label: "Create role action", Path: "/admin/roles", Permission: "admin.roles.manage"}, admin.roleCreate},
		{"GET /admin/roles/{id}", catalog.Page{ID: "roles.edit", Label: "Edit role", Path: "/admin/roles/{id}", Permission: "admin.roles.manage"}, admin.roleEdit},
		{"POST /admin/roles/{id}", catalog.Page{ID: "roles.update", Label: "Update role action", Path: "/admin/roles/{id}", Permission: "admin.roles.manage"}, admin.roleUpdate},
		{"POST /admin/roles/{id}/delete", catalog.Page{ID: "roles.delete", Label: "Delete role action", Path: "/admin/roles/{id}/delete", Permission: "admin.roles.manage"}, admin.roleDelete},
		{"GET /admin/pages", catalog.Page{ID: "pages", Label: "Pages", Path: "/admin/pages", Description: "Automatically registered public and admin pages", Permission: "admin.pages.view", Navigation: true, Order: 40}, admin.pageList},
		{"GET /admin/audit", catalog.Page{ID: "audit", Label: "Audit log", Path: "/admin/audit", Description: "Administrative activity history", Permission: "admin.audit.view", Navigation: true, Order: 50}, admin.auditLog},
		{"GET /admin/audit/export", catalog.Page{ID: "audit.export", Label: "Export audit log", Path: "/admin/audit/export", Description: "Download filtered audit records", Permission: "admin.audit.view"}, admin.auditExport},
		{"GET /admin/emails", catalog.Page{ID: "emails", Label: "Email", Path: "/admin/emails", Description: "Inspect queued and delivered email", Permission: "admin.email.view", Navigation: true, Order: 55}, admin.emails},
		{"POST /admin/emails/{id}/retry", catalog.Page{ID: "emails.retry", Label: "Retry email", Path: "/admin/emails/{id}/retry", Permission: "admin.email.manage"}, admin.emailRetry},
		{"GET /admin/jobs", catalog.Page{ID: "jobs", Label: "Jobs", Path: "/admin/jobs", Description: "Scheduled background work and status", Permission: "admin.settings.manage", Navigation: true, Order: 56}, admin.jobsList},
		{"POST /admin/jobs/{id}/run", catalog.Page{ID: "jobs.run", Label: "Run job", Path: "/admin/jobs/{id}/run", Permission: "admin.settings.manage"}, admin.jobRun},
		{"POST /admin/jobs/{id}", catalog.Page{ID: "jobs.update", Label: "Configure job", Path: "/admin/jobs/{id}", Permission: "admin.settings.manage"}, admin.jobConfigure},
		{"GET /admin/backups", catalog.Page{ID: "backups", Label: "Backups", Path: "/admin/backups", Description: "Create, verify, and download backups", Permission: "admin.settings.manage", Navigation: true, Order: 58}, admin.backupsList},
		{"POST /admin/backups", catalog.Page{ID: "backups.create", Label: "Create backup", Path: "/admin/backups", Permission: "admin.settings.manage"}, admin.backupCreate},
		{"GET /admin/backups/{name}", catalog.Page{ID: "backups.download", Label: "Download backup", Path: "/admin/backups/{name}", Permission: "admin.settings.manage"}, admin.backupDownload},
		{"POST /admin/backups/{name}/verify", catalog.Page{ID: "backups.verify", Label: "Verify backup", Path: "/admin/backups/{name}/verify", Permission: "admin.settings.manage"}, admin.backupVerify},
		{"GET /admin/rate-limits", catalog.Page{ID: "rate-limits", Label: "Rate limits", Path: "/admin/rate-limits", Description: "Inspect and clear shared request limits", Permission: "admin.rate-limits.view", Navigation: true, Order: 59}, admin.rateLimits},
		{"POST /admin/rate-limits/{scope}/{hash}/clear", catalog.Page{ID: "rate-limits.clear", Label: "Clear rate limit", Path: "/admin/rate-limits/{scope}/{hash}/clear", Permission: "admin.rate-limits.manage"}, admin.rateLimitClear},
		{"GET /admin/media", catalog.Page{ID: "media", Label: "Media", Path: "/admin/media", Description: "Upload and manage application files", Permission: "admin.media.view", Navigation: true, Order: 57}, admin.mediaList},
		{"POST /admin/media", catalog.Page{ID: "media.upload", Label: "Upload media", Path: "/admin/media", Permission: "admin.media.manage"}, admin.mediaUpload},
		{"POST /admin/media/bulk-delete", catalog.Page{ID: "media.bulk-delete", Label: "Delete selected media", Path: "/admin/media/bulk-delete", Permission: "admin.media.manage"}, admin.mediaBulkDelete},
		{"GET /admin/media/{id}/edit", catalog.Page{ID: "media.edit", Label: "Edit media", Path: "/admin/media/{id}/edit", Permission: "admin.media.manage"}, admin.mediaEdit},
		{"POST /admin/media/{id}", catalog.Page{ID: "media.update", Label: "Update media", Path: "/admin/media/{id}", Permission: "admin.media.manage"}, admin.mediaUpdate},
		{"GET /admin/media/{id}", catalog.Page{ID: "media.download", Label: "View media", Path: "/admin/media/{id}", Permission: "admin.media.view"}, admin.mediaDownload},
		{"POST /admin/media/{id}/delete", catalog.Page{ID: "media.delete", Label: "Delete media", Path: "/admin/media/{id}/delete", Permission: "admin.media.manage"}, admin.mediaDelete},
		{"GET /admin/media/cleanup", catalog.Page{ID: "media.cleanup", Label: "Media cleanup", Path: "/admin/media/cleanup", Permission: "admin.media.manage"}, admin.mediaCleanup},
		{"POST /admin/media/cleanup/settings", catalog.Page{ID: "media.cleanup.settings", Label: "Media cleanup settings", Path: "/admin/media/cleanup/settings", Permission: "admin.media.manage"}, admin.mediaCleanupSave},
		{"POST /admin/media/cleanup/run", catalog.Page{ID: "media.cleanup.run", Label: "Run media cleanup", Path: "/admin/media/cleanup/run", Permission: "admin.media.manage"}, admin.mediaCleanupRun},
		{"GET /admin/settings", catalog.Page{ID: "settings", Label: "Settings", Path: "/admin/settings", Description: "Basic application settings", Permission: "admin.settings.manage", Navigation: true, Order: 60}, admin.settings},
		{"POST /admin/settings", catalog.Page{ID: "settings.update", Label: "Update settings action", Path: "/admin/settings", Permission: "admin.settings.manage"}, admin.settingsUpdate},
		{"POST /admin/settings/test-email", catalog.Page{ID: "settings.test-email", Label: "Send test email", Path: "/admin/settings/test-email", Permission: "admin.settings.manage"}, admin.emailTest},
	}
	for _, route := range routes {
		if !admin.featureEnabled(route.page.ID) {
			continue
		}
		if err := admin.Handle(route.pattern, route.page, admin.handler(route.page.ID, route.handler)); err != nil {
			return err
		}
	}
	return nil
}

func (admin *Admin) featureEnabled(pageID string) bool {
	switch pageID {
	case "dashboard":
		return admin.features.Dashboard
	case "users", "users.new", "users.create", "users.edit", "users.update", "users.delete", "users.revoke-sessions", "users.avatar", "users.avatar.update", "users.avatar.remove":
		return admin.features.Users
	case "roles", "roles.new", "roles.create", "roles.edit", "roles.update", "roles.delete":
		return admin.features.Roles
	case "pages":
		return admin.features.Pages
	case "audit", "audit.export":
		return admin.features.Audit
	case "emails", "emails.retry":
		return admin.features.Email
	case "jobs", "jobs.run", "jobs.update":
		return admin.features.Settings
	case "backups", "backups.create", "backups.download", "backups.verify":
		return admin.features.Settings && admin.backups != nil
	case "rate-limits", "rate-limits.clear":
		return admin.features.Settings
	case "media", "media.upload", "media.bulk-delete", "media.edit", "media.update", "media.download", "media.delete", "media.cleanup", "media.cleanup.settings", "media.cleanup.run":
		return admin.features.Media
	case "settings", "settings.update", "settings.test-email":
		return admin.features.Settings
	default:
		return true
	}
}
