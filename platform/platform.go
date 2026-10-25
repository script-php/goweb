// Package platform exposes the stable extension API used by individual
// websites. Packages under internal/ are implementation details.
package platform

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"script-php/goweb/internal/auth"
	"script-php/goweb/internal/clientip"
	"script-php/goweb/internal/jobs"
	appmail "script-php/goweb/internal/mail"
	appmedia "script-php/goweb/internal/media"
	"script-php/goweb/internal/ratelimit"
	"script-php/goweb/internal/server"
	serveradmin "script-php/goweb/internal/server/admin"
	"script-php/goweb/internal/server/catalog"
	"script-php/goweb/platform/configuration"
)

//go:embed web database
var builtins embed.FS

type Config = config.Config
type Page struct {
	ID, Label, Path, Description, Permission string
	Navigation                               bool
	Order                                    int
}

func (page Page) internal() catalog.Page {
	return catalog.Page{ID: page.ID, Label: page.Label, Path: page.Path, Description: page.Description, Permission: page.Permission, Navigation: page.Navigation, Order: page.Order}
}

type Pages struct{ inner *server.Public }

func (pages *Pages) Handle(pattern string, page Page, handler http.Handler) error {
	return pages.inner.Handle(pattern, page.internal(), handler)
}
func (pages *Pages) Template(pattern string, page Page, name, title string, data any) error {
	return pages.inner.Template(pattern, page.internal(), name, title, data)
}

type User struct {
	ID, AvatarMediaID           uint64
	Email, DisplayName          string
	Active, ForcePasswordChange bool
	Roles                       []uint64
	RoleNames                   []string
}
type DashboardWidget struct {
	ID, Title string
	Render    func(*http.Request) (template.HTML, error)
}
type UserFormSection struct {
	ID, Title string
	Render    func(*http.Request, User) (template.HTML, error)
	Save      func(*http.Request, uint64) error
}
type Admin struct{ inner *serveradmin.Admin }

func (admin *Admin) Handle(pattern string, page Page, handler http.Handler) error {
	return admin.inner.Handle(pattern, page.internal(), handler)
}
func (admin *Admin) Render(w http.ResponseWriter, r *http.Request, status int, page, title string, data any) {
	admin.inner.Render(w, r, status, page, title, data)
}
func (admin *Admin) Override(id string, handler http.Handler) error {
	return admin.inner.Override(id, handler)
}
func (admin *Admin) AddDashboardWidget(widget DashboardWidget) error {
	return admin.inner.AddDashboardWidget(serveradmin.DashboardWidget(widget))
}
func (admin *Admin) AddUserFormSection(section UserFormSection) error {
	value := serveradmin.UserFormSection{ID: section.ID, Title: section.Title, Save: section.Save}
	if section.Render != nil {
		value.Render = func(r *http.Request, user serveradmin.User) (template.HTML, error) {
			return section.Render(r, User{ID: user.ID, AvatarMediaID: user.AvatarMediaID, Email: user.Email, DisplayName: user.DisplayName, Active: user.Active, ForcePasswordChange: user.ForcePasswordChange, Roles: user.Roles, RoleNames: user.RoleNames})
		}
	}
	return admin.inner.AddUserFormSection(value)
}

const (
	PageLogin          = "login"
	PageLoginSubmit    = "login.submit"
	PageLogout         = "logout"
	PageAccount        = "account"
	PageForgotPassword = "forgot-password"
	PageResetPassword  = "reset-password"
	PageVerifyEmail    = "verify-email"
	PageTwoFactor      = "two-factor"
	PageChangePassword = "change-password"
	AdminDashboard     = "dashboard"
	AdminUsers         = "users"
	AdminUserCreate    = "users.create"
	AdminUserEdit      = "users.edit"
	AdminRoles         = "roles"
	AdminPages         = "pages"
	AdminAudit         = "audit"
	AdminEmail         = "emails"
	AdminMedia         = "media"
	AdminRateLimits    = "rate-limits"
	AdminSettings      = "settings"
)

type Features struct{ Dashboard, Users, Roles, Pages, Audit, Email, Media, Settings bool }

func DefaultFeatures() Features { return Features{true, true, true, true, true, true, true, true} }

type BackgroundJob struct {
	ID, Label string
	Interval  time.Duration
	Enabled   bool
	Run       func(context.Context) error
}

// RateLimitPolicy applies a database-shared fixed-window limit to matching
// requests. Match selects requests and Key identifies the caller or resource.
type RateLimitPolicy struct {
	ID     string
	Limit  int
	Window time.Duration
	Match  func(*http.Request) bool
	Key    func(*http.Request) string
}

// RateLimitByIP groups requests by the safely resolved client address.
func RateLimitByIP(r *http.Request) string { return clientip.FromRequest(r) }

// RateLimitByUser groups authenticated users by ID and falls back to client IP.
func RateLimitByUser(r *http.Request) string {
	if user := auth.CurrentUser(r.Context()); user != nil {
		return "user:" + strconv.FormatUint(user.ID, 10)
	}
	return "ip:" + clientip.FromRequest(r)
}

type Options struct {
	DB              *sql.DB
	Config          Config
	Version         string
	Resources       Resources
	Schemas         Schemas
	Features        Features
	RegisterPages   func(*Pages) error
	RegisterAdmin   func(*Admin) error
	PublicOverrides map[string]http.Handler
	AdminOverrides  map[string]http.Handler
	MediaBackend    StorageBackend
	MediaReferences []MediaReferenceRule
	BackgroundJobs  []BackgroundJob
	RateLimits      []RateLimitPolicy
	Backups         *BackupOperations
}

type Runtime struct {
	Handler http.Handler
	queue   *appmail.Queue
	media   *appmedia.Service
	jobs    *jobs.Manager
	config  Config
	Media   *Media
}

func (runtime *Runtime) RunWorkers(ctx context.Context) { runtime.queue.Run(ctx) }

func (runtime *Runtime) Run(ctx context.Context) error {
	workerContext, stopWorkers := context.WithCancel(ctx)
	defer stopWorkers()
	go runtime.queue.Run(workerContext)
	go runtime.jobs.Run(workerContext)
	server := &http.Server{Addr: runtime.config.Server.Address, Handler: runtime.Handler,
		ReadHeaderTimeout: runtime.config.Server.ReadHeaderTimeout.Duration, ReadTimeout: runtime.config.Server.ReadTimeout.Duration,
		WriteTimeout: runtime.config.Server.WriteTimeout.Duration, IdleTimeout: runtime.config.Server.IdleTimeout.Duration}
	errorsChannel := make(chan error, 1)
	go func() { errorsChannel <- server.ListenAndServe() }()
	select {
	case err := <-errorsChannel:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve website: %w", err)
		}
		return nil
	case <-ctx.Done():
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), runtime.config.Server.ShutdownTimeout.Duration)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shut down website: %w", err)
	}
	err := <-errorsChannel
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func New(options Options) (*Runtime, error) {
	files, err := resourceFiles(options.Resources)
	if err != nil {
		return nil, err
	}
	migrationContext := context.Background()
	var cancel context.CancelFunc = func() {}
	if options.Config.Database.MigrationTimeout.Duration > 0 {
		migrationContext, cancel = context.WithTimeout(migrationContext, options.Config.Database.MigrationTimeout.Duration)
	}
	defer cancel()
	if err := ApplySchemas(migrationContext, options.DB, options.Schemas); err != nil {
		return nil, err
	}
	backend := options.MediaBackend
	if backend == nil {
		local, err := appmedia.NewLocalBackend(options.Config.Storage.UploadsDirectory)
		if err != nil {
			return nil, fmt.Errorf("prepare media storage: %w", err)
		}
		backend = local
	} else if lister, ok := backend.(StorageObjectLister); ok {
		backend = listingStorageBackend{StorageBackend: backend, lister: lister}
	}
	mediaService := appmedia.New(options.DB, backend, options.Config.Storage.MaxUploadBytes, options.Config.Storage.AllowedMIMETypes)
	rules := make([]appmedia.ReferenceRule, 1, len(options.MediaReferences)+1)
	rules[0] = appmedia.ReferenceRule{ID: "user_avatar", Label: "User avatar", Table: "users", Column: "avatar_media_id"}
	for _, rule := range options.MediaReferences {
		rules = append(rules, appmedia.ReferenceRule{ID: rule.ID, Label: rule.Label, Table: rule.Table, Column: rule.Column})
	}
	if err := mediaService.ConfigureCleanupRules(rules); err != nil {
		return nil, err
	}
	smtp := appmail.NewSMTP(options.DB, options.Config.Email)
	queue := appmail.NewQueue(options.DB, smtp)
	rateLimitStore := ratelimit.New(options.DB)
	registeredJobs := []jobs.Job{
		{ID: "platform.media_cleanup", Label: "Media cleanup", Interval: time.Hour, Enabled: true, Run: mediaService.RunConfiguredCleanup},
		{ID: "platform.email_cleanup", Label: "Email history cleanup", Interval: 24 * time.Hour, Enabled: true, Run: queue.Cleanup},
		{ID: "platform.rate_limit_cleanup", Label: "Rate-limit cleanup", Interval: 24 * time.Hour, Enabled: true, Run: func(ctx context.Context) error {
			return rateLimitStore.Cleanup(ctx, time.Now().Add(-7*24*time.Hour))
		}},
	}
	registeredRateLimits := make([]ratelimit.Policy, 0, len(options.RateLimits))
	for _, policy := range options.RateLimits {
		registeredRateLimits = append(registeredRateLimits, ratelimit.Policy(policy))
	}
	for _, job := range options.BackgroundJobs {
		registeredJobs = append(registeredJobs, jobs.Job{ID: job.ID, Label: job.Label, Interval: job.Interval, Enabled: job.Enabled, Run: job.Run})
	}
	jobManager, err := jobs.New(options.DB, registeredJobs)
	if err != nil {
		return nil, err
	}
	authentication := auth.NewService(options.DB, auth.ServiceOptions{
		SessionLength:    options.Config.Authentication.SessionLifetime.Duration,
		SecureCookies:    options.Config.Authentication.SecureCookies,
		MaxLoginAttempts: options.Config.Authentication.LoginMaxAttempts,
		LoginLockout:     options.Config.Authentication.LoginLockout.Duration,
		Mailer:           queue, PublicURL: options.Config.Server.PublicURL, EmailEnabled: options.Config.Email.Enabled,
	})
	features := options.Features
	if features == (Features{}) {
		features = DefaultFeatures()
	}
	adminFeatures := serveradmin.Features(features)
	serverOptions := []server.Option{
		server.WithAuthentication(authentication), server.WithTrustedProxies(options.Config.Server.TrustedProxies),
		server.WithEmailConfiguration(options.Config.Email), server.WithAdminFeatures(adminFeatures),
		server.WithPublicOverrides(options.PublicOverrides),
		server.WithMedia(mediaService),
		server.WithJobs(jobManager),
		server.WithRateLimits(registeredRateLimits),
	}
	if options.Backups != nil {
		serverOptions = append(serverOptions, server.WithBackups(backupOperationsAdapter(options.Backups)))
	}
	if options.RegisterPages != nil {
		serverOptions = append(serverOptions, server.WithPublicModule(publicRegistration(options.RegisterPages)))
	}
	if options.RegisterAdmin != nil || len(options.AdminOverrides) > 0 {
		serverOptions = append(serverOptions, server.WithAdminModule(adminRegistration{register: options.RegisterAdmin, overrides: options.AdminOverrides}))
	}
	handler, err := server.New(files, options.DB, options.Version, serverOptions...)
	if err != nil {
		return nil, err
	}
	return &Runtime{Handler: handler, queue: queue, media: mediaService, jobs: jobManager, config: options.Config, Media: &Media{inner: mediaService}}, nil
}

type publicRegistration func(*Pages) error

func (registration publicRegistration) RegisterPublic(pages *server.Public) error {
	return registration(&Pages{inner: pages})
}

type adminRegistration struct {
	register  func(*Admin) error
	overrides map[string]http.Handler
}

func (registration adminRegistration) Register(admin *serveradmin.Admin) error {
	for id, handler := range registration.overrides {
		if err := admin.Override(id, handler); err != nil {
			return err
		}
	}
	if registration.register != nil {
		return registration.register(&Admin{inner: admin})
	}
	return nil
}

// HTML marks trusted extension output. Prefer templates for substantial pages;
// widgets and form sections may use this helper for small fragments.
func HTML(value string) template.HTML { return template.HTML(value) }
