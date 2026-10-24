// Package admin provides an extensible administration area. Built-in modules
// cover the dashboard, users, roles/permissions, page catalog, and settings.
package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"script-php/goweb/internal/auth"
	"script-php/goweb/internal/jobs"
	appmedia "script-php/goweb/internal/media"
	"script-php/goweb/internal/metrics"
	"script-php/goweb/internal/server/catalog"
	"script-php/goweb/platform/configuration"
)

type Module interface {
	Register(*Admin) error
}

type Admin struct {
	mux           *http.ServeMux
	store         *Store
	catalog       *catalog.Catalog
	renderer      *renderer
	metrics       *metrics.Collector
	emailDefaults config.Email
	media         *appmedia.Service
	jobs          *jobs.Manager
	backups       *BackupOperations
	overrides     map[string]http.Handler
	widgets       []DashboardWidget
	userSections  []UserFormSection
	features      Features
}

type Features struct{ Dashboard, Users, Roles, Pages, Audit, Email, Media, Settings bool }

func DefaultFeatures() Features { return Features{true, true, true, true, true, true, true, true} }

type DashboardWidget struct {
	ID, Title string
	Render    func(*http.Request) (template.HTML, error)
}

type UserFormSection struct {
	ID, Title string
	Render    func(*http.Request, User) (template.HTML, error)
	Save      func(*http.Request, uint64) error
}

func New(db *sql.DB, webFiles fs.FS, version string, pageCatalog *catalog.Catalog, resourceMetrics *metrics.Collector, emailDefaults config.Email, mediaService *appmedia.Service, jobManager *jobs.Manager, backups *BackupOperations, features Features, modules ...Module) (*Admin, error) {
	renderer, err := newRenderer(webFiles, version, pageCatalog)
	if err != nil {
		return nil, err
	}
	admin := &Admin{
		mux:           http.NewServeMux(),
		store:         NewStore(db),
		catalog:       pageCatalog,
		renderer:      renderer,
		metrics:       resourceMetrics,
		emailDefaults: emailDefaults,
		media:         mediaService,
		jobs:          jobManager,
		backups:       backups,
		overrides:     make(map[string]http.Handler),
		features:      features,
	}
	// Application modules register extension points and overrides before the
	// built-in routes are finalized.
	for _, module := range modules {
		if err := module.Register(admin); err != nil {
			return nil, err
		}
	}
	if err := (builtinModule{}).Register(admin); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := admin.store.SyncPermissions(ctx, pageCatalog.Permissions()); err != nil {
		return nil, err
	}
	if settings, err := admin.store.Settings(ctx); err != nil {
		return nil, err
	} else {
		days := 365
		if configured, parseErr := strconv.Atoi(settings["audit.retention_days"]); parseErr == nil && configured >= 1 && configured <= 3650 {
			days = configured
		}
		if _, err := admin.store.DeleteAuditBefore(ctx, time.Now().UTC().AddDate(0, 0, -days)); err != nil {
			return nil, fmt.Errorf("clean audit log: %w", err)
		}
	}
	admin.mux.Handle("/admin/", auth.RequireLogin("/login", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin.RenderError(w, r, http.StatusNotFound)
	})))
	return admin, nil
}

func (admin *Admin) Override(pageID string, handler http.Handler) error {
	if pageID == "" || handler == nil {
		return errors.New("override page ID and handler are required")
	}
	if _, exists := admin.overrides[pageID]; exists {
		return fmt.Errorf("admin page %q already has an override", pageID)
	}
	admin.overrides[pageID] = handler
	return nil
}

func (admin *Admin) handler(pageID string, fallback http.Handler) http.Handler {
	if replacement := admin.overrides[pageID]; replacement != nil {
		return replacement
	}
	return fallback
}

func (admin *Admin) AddDashboardWidget(widget DashboardWidget) error {
	if widget.ID == "" || widget.Title == "" || widget.Render == nil {
		return errors.New("dashboard widget ID, title, and renderer are required")
	}
	for _, existing := range admin.widgets {
		if existing.ID == widget.ID {
			return fmt.Errorf("dashboard widget %q already registered", widget.ID)
		}
	}
	admin.widgets = append(admin.widgets, widget)
	return nil
}

func (admin *Admin) AddUserFormSection(section UserFormSection) error {
	if section.ID == "" || section.Title == "" || section.Render == nil {
		return errors.New("user form section ID, title, and renderer are required")
	}
	for _, existing := range admin.userSections {
		if existing.ID == section.ID {
			return fmt.Errorf("user form section %q already registered", section.ID)
		}
	}
	admin.userSections = append(admin.userSections, section)
	return nil
}

// Handle lets future feature modules add protected admin pages. Metadata is
// automatically reflected in navigation, the page catalog, and permissions.
func (admin *Admin) Handle(pattern string, page catalog.Page, handler http.Handler) error {
	page.Area = catalog.AreaAdmin
	if err := admin.catalog.Register(page); err != nil {
		return err
	}
	if page.Permission != "" {
		handler = auth.RequirePermission(page.Permission, handler)
	}
	handler = auth.RequireLogin("/login", handler)
	admin.mux.Handle(pattern, handler)
	return nil
}

func (admin *Admin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sameOrigin(admin.mux).ServeHTTP(w, r)
}

// Render renders a feature module's template. Put the template at
// templates/admin/pages/<page>.html in the configured application resources.
func (admin *Admin) Render(w http.ResponseWriter, r *http.Request, status int, page, title string, data any) {
	admin.renderer.render(w, r, status, page, title, data, "")
}

func (admin *Admin) RenderError(w http.ResponseWriter, r *http.Request, status int) {
	message := "The requested operation could not be completed."
	switch status {
	case http.StatusNotFound:
		message = "The requested admin page does not exist."
	case http.StatusForbidden:
		message = "Your account does not have permission to perform this action."
	case http.StatusInternalServerError:
		message = "The error has been recorded. Please try again."
	}
	admin.renderer.render(w, r, status, "error", http.StatusText(status), errorData{Status: status, Title: http.StatusText(status), Message: message}, "")
}

func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "same-site" && site != "none" {
			auth.RespondError(w, r, http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			if parsed, err := url.Parse(origin); err != nil || !strings.EqualFold(parsed.Host, r.Host) {
				auth.RespondError(w, r, http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type renderer struct {
	pages   map[string]*template.Template
	version string
	catalog *catalog.Catalog
}

type viewData struct {
	Title         string
	Version       string
	CurrentUser   *auth.User
	Navigation    []catalog.Page
	Data          any
	Notifications []notification
	CSRFToken     string
	Breadcrumbs   []breadcrumb
}

type notification struct{ Kind, Message string }

type breadcrumb struct {
	Label string
	Path  string
}

type errorData struct {
	Status  int
	Title   string
	Message string
}

func newRenderer(webFiles fs.FS, version string, pageCatalog *catalog.Catalog) (*renderer, error) {
	pageFiles, err := fs.Glob(webFiles, "templates/admin/pages/*.html")
	if err != nil {
		return nil, err
	}
	if len(pageFiles) == 0 {
		return nil, fmt.Errorf("no admin templates found")
	}
	pages := make(map[string]*template.Template, len(pageFiles))
	functions := template.FuncMap{
		"dict": func(values ...any) map[string]any {
			result := make(map[string]any, len(values)/2)
			for index := 0; index+1 < len(values); index += 2 {
				key, _ := values[index].(string)
				result[key] = values[index+1]
			}
			return result
		},
		"containsID": func(ids []uint64, id uint64) bool {
			for _, value := range ids {
				if value == id {
					return true
				}
			}
			return false
		},
		"contains": func(values []string, value string) bool {
			for _, item := range values {
				if item == value {
					return true
				}
			}
			return false
		},
		"hasPrefix": strings.HasPrefix,
	}
	for _, file := range pageFiles {
		name := strings.TrimSuffix(strings.TrimPrefix(file, "templates/admin/pages/"), ".html")
		parsed, err := template.New("base.html").Funcs(functions).ParseFS(webFiles, "templates/admin/base.html", "templates/admin/components.html", file)
		if err != nil {
			return nil, fmt.Errorf("parse admin template %s: %w", file, err)
		}
		pages[name] = parsed
	}
	return &renderer{pages: pages, version: version, catalog: pageCatalog}, nil
}

func (renderer *renderer) render(w http.ResponseWriter, r *http.Request, status int, page, title string, data any, message string) {
	pageTemplate, exists := renderer.pages[page]
	if !exists {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	navigation := renderer.catalog.Pages(catalog.AreaAdmin)
	user := auth.CurrentUser(r.Context())
	filtered := navigation[:0]
	for _, item := range navigation {
		if item.Navigation && (item.Permission == "" || user != nil && user.HasPermission(item.Permission)) {
			filtered = append(filtered, item)
		}
	}
	breadcrumbs := []breadcrumb{{Label: "Dashboard", Path: "/admin/"}}
	if r.URL.Path != "/admin/" {
		breadcrumbs = append(breadcrumbs, breadcrumb{Label: title})
	}
	dataView := viewData{Title: title, Version: renderer.version, CurrentUser: user, Navigation: filtered, Data: data, Notifications: notifications(r, message), CSRFToken: auth.CSRFToken(r.Context()), Breadcrumbs: breadcrumbs}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := pageTemplate.ExecuteTemplate(w, "admin-base", dataView); err != nil {
		return
	}
}

func notifications(r *http.Request, errorMessage string) []notification {
	result := make([]notification, 0, 2)
	if errorMessage != "" {
		result = append(result, notification{Kind: "error", Message: errorMessage})
	}
	if message := strings.TrimSpace(r.URL.Query().Get("notice")); message != "" {
		kind := r.URL.Query().Get("notice_type")
		if kind != "warning" && kind != "error" {
			kind = "success"
		}
		result = append(result, notification{Kind: kind, Message: message})
	}
	return result
}

func normalizeSlug(value string) string {
	return strings.Trim(strings.ToLower(strings.ReplaceAll(value, " ", "-")), "-")
}

func sortedSettings(settings map[string]string) []settingField {
	fields := make([]settingField, 0, len(settings))
	for key, value := range settings {
		fields = append(fields, settingField{Key: key, Value: value})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Key < fields[j].Key })
	return fields
}
