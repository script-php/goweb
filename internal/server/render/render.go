// Package render owns template discovery, parsing, and HTML responses.
package render

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"

	"script-php/goweb/internal/auth"
	"script-php/goweb/internal/server/catalog"
)

type Renderer struct {
	pages    map[string]*template.Template
	version  string
	catalog  *catalog.Catalog
	settings SettingsProvider
}

type SettingsProvider interface {
	Settings(context.Context) (map[string]string, error)
}

type pageData struct {
	Title       string
	Version     string
	CurrentUser *auth.User
	Navigation  []catalog.Page
	Settings    map[string]string
	CSRFToken   string
	Data        any
}

func New(webFiles fs.FS, version string, pageCatalog *catalog.Catalog, settings SettingsProvider) (*Renderer, error) {
	pageFiles, err := fs.Glob(webFiles, "templates/pages/*.html")
	if err != nil {
		return nil, fmt.Errorf("find page templates: %w", err)
	}
	if len(pageFiles) == 0 {
		return nil, fmt.Errorf("no page templates found")
	}

	pageTemplates := make(map[string]*template.Template, len(pageFiles))
	for _, pageFile := range pageFiles {
		name := strings.TrimSuffix(filepath.Base(pageFile), filepath.Ext(pageFile))
		parsed, err := template.ParseFS(webFiles, "templates/base.html", pageFile)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", pageFile, err)
		}
		pageTemplates[name] = parsed
	}

	return &Renderer{pages: pageTemplates, version: version, catalog: pageCatalog, settings: settings}, nil
}

func (renderer *Renderer) Render(w http.ResponseWriter, r *http.Request, status int, page, title string, data any) {
	pageTemplate, exists := renderer.pages[page]
	if !exists {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	applicationSettings := map[string]string{}
	if renderer.settings != nil {
		if loaded, err := renderer.settings.Settings(r.Context()); err == nil {
			applicationSettings = loaded
		}
	}
	viewData := pageData{
		Title:       title,
		Version:     renderer.version,
		CurrentUser: auth.CurrentUser(r.Context()),
		Navigation:  renderer.catalog.Pages(catalog.AreaPublic),
		Settings:    applicationSettings,
		CSRFToken:   auth.CSRFToken(r.Context()),
		Data:        data,
	}
	var output bytes.Buffer
	if err := pageTemplate.ExecuteTemplate(&output, "base", viewData); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = output.WriteTo(w)
}

func (renderer *Renderer) RenderError(w http.ResponseWriter, r *http.Request, status int) {
	title := http.StatusText(status)
	page := fmt.Sprintf("error-%d", status)
	if _, exists := renderer.pages[page]; !exists {
		http.Error(w, title, status)
		return
	}
	renderer.Render(w, r, status, page, title, nil)
}
