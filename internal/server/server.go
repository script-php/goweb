package server

import (
	"database/sql"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"script-php/goweb/internal/auth"
	"script-php/goweb/internal/clientip"
	"script-php/goweb/internal/jobs"
	appmedia "script-php/goweb/internal/media"
	"script-php/goweb/internal/metrics"
	"script-php/goweb/internal/ratelimit"
	"script-php/goweb/internal/server/admin"
	"script-php/goweb/internal/server/catalog"
	"script-php/goweb/internal/server/pages"
	"script-php/goweb/internal/server/render"
	"script-php/goweb/platform/configuration"
)

type options struct {
	sessionResolver auth.SessionResolver
	authService     *auth.Service
	adminModules    []admin.Module
	trustedProxies  []string
	emailDefaults   config.Email
	publicModules   []PublicModule
	publicOverrides map[string]http.Handler
	adminFeatures   admin.Features
	media           *appmedia.Service
	jobs            *jobs.Manager
	backups         *admin.BackupOperations
	rateLimits      []ratelimit.Policy
	readiness       *Readiness
}

type PublicModule interface{ RegisterPublic(*Public) error }

type Option func(*options)

func WithSessionResolver(resolver auth.SessionResolver) Option {
	return func(configuration *options) { configuration.sessionResolver = resolver }
}

func WithAuthentication(service *auth.Service) Option {
	return func(configuration *options) {
		configuration.authService = service
		configuration.sessionResolver = service
	}
}

func WithAdminModule(module admin.Module) Option {
	return func(configuration *options) { configuration.adminModules = append(configuration.adminModules, module) }
}

func WithTrustedProxies(proxies []string) Option {
	return func(configuration *options) { configuration.trustedProxies = append([]string(nil), proxies...) }
}

func WithEmailConfiguration(email config.Email) Option {
	return func(configuration *options) { configuration.emailDefaults = email }
}

func WithPublicModule(module PublicModule) Option {
	return func(configuration *options) {
		configuration.publicModules = append(configuration.publicModules, module)
	}
}
func WithPublicOverrides(overrides map[string]http.Handler) Option {
	return func(configuration *options) { configuration.publicOverrides = overrides }
}
func WithAdminFeatures(features admin.Features) Option {
	return func(configuration *options) { configuration.adminFeatures = features }
}
func WithMedia(service *appmedia.Service) Option {
	return func(configuration *options) { configuration.media = service }
}
func WithJobs(manager *jobs.Manager) Option {
	return func(configuration *options) { configuration.jobs = manager }
}
func WithBackups(operations *admin.BackupOperations) Option {
	return func(configuration *options) { configuration.backups = operations }
}
func WithRateLimits(policies []ratelimit.Policy) Option {
	return func(configuration *options) { configuration.rateLimits = append([]ratelimit.Policy(nil), policies...) }
}
func WithReadiness(readiness *Readiness) Option {
	return func(configuration *options) { configuration.readiness = readiness }
}

func New(webFiles fs.FS, db *sql.DB, version string, suppliedOptions ...Option) (http.Handler, error) {
	configuration := options{adminFeatures: admin.DefaultFeatures()}
	for _, option := range suppliedOptions {
		option(&configuration)
	}
	if configuration.readiness == nil {
		configuration.readiness = NewReadiness()
	}
	clientAddresses, err := clientip.New(configuration.trustedProxies)
	if err != nil {
		return nil, err
	}
	if err := ratelimit.ValidatePolicies(configuration.rateLimits); err != nil {
		return nil, err
	}
	pageCatalog := catalog.New()
	resourceMetrics := metrics.New()
	renderer, err := render.New(webFiles, version, pageCatalog, admin.NewStore(db))
	if err != nil {
		return nil, err
	}

	staticFiles, err := fs.Sub(webFiles, "static")
	if err != nil {
		return nil, err
	}

	pageHandlers := pages.New(renderer, configuration.authService)
	mux := http.NewServeMux()
	public := &Public{mux: mux, catalog: pageCatalog, pages: pageHandlers, renderer: renderer, overrides: configuration.publicOverrides}
	if err := registerPublicRoutes(public); err != nil {
		return nil, err
	}
	if configuration.media != nil {
		registerPublicAvatars(mux, db, configuration.media)
	}
	for _, module := range configuration.publicModules {
		if err := module.RegisterPublic(public); err != nil {
			return nil, err
		}
	}
	if configuration.media == nil {
		configuration.adminFeatures.Media = false
	}
	adminArea, err := admin.New(db, webFiles, version, pageCatalog, resourceMetrics, configuration.emailDefaults, configuration.media, configuration.jobs, configuration.backups, configuration.adminFeatures, configuration.adminModules...)
	if err != nil {
		return nil, err
	}
	mux.Handle("/admin/", adminArea)
	registerPrometheusMetrics(mux, resourceMetrics, db)
	registerInfrastructureRoutes(mux, db, staticFiles, version, configuration.readiness)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		renderer.RenderError(w, r, http.StatusNotFound)
	})
	var handler http.Handler = mux
	secureCookies := false
	if configuration.authService != nil {
		secureCookies = configuration.authService.SecureCookies()
	}
	maxRequestBytes := int64(11 << 20)
	if configuration.media != nil {
		maxRequestBytes = configuration.media.MaxBytes() + (1 << 20)
	}
	handler = auth.ProtectCSRFWithMaxBytes(secureCookies, maxRequestBytes, handler)
	handler = recoverPanics(handler)
	handler = requestLogging(handler)
	if len(configuration.rateLimits) > 0 {
		handler = ratelimit.New(db).Middleware(configuration.rateLimits, auth.RespondError, handler)
	}
	if configuration.sessionResolver != nil {
		handler = auth.RequirePasswordChange(handler)
		handler = auth.Authenticate(auth.SessionCookieName, configuration.sessionResolver, handler)
	}
	handler = recoverPanics(handler)
	handler = identifyRequest(handler)
	handler = auth.WithErrorResponder(handler, func(w http.ResponseWriter, r *http.Request, status int) {
		if strings.HasPrefix(r.URL.Path, "/admin/") {
			adminArea.RenderError(w, r, status)
			return
		}
		renderer.RenderError(w, r, status)
	})
	handler = databaseMaintenance(db, 2*time.Second, handler)
	handler = securityHeaders(handler)
	handler = resourceMetrics.Middleware(handler)
	handler = clientAddresses.Middleware(handler)
	return handler, nil
}
