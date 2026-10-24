// Package catalog is the shared registry for public and administrative pages.
// Register route metadata here once; routing, navigation, and the admin page
// catalog can all derive from the same source.
package catalog

import (
	"fmt"
	"sort"
	"sync"
)

type Area string

const (
	AreaPublic Area = "public"
	AreaAdmin  Area = "admin"
)

type Page struct {
	ID          string
	Area        Area
	Label       string
	Path        string
	Description string
	Permission  string
	Navigation  bool
	Order       int
}

type Catalog struct {
	mu    sync.RWMutex
	pages map[string]Page
}

func New() *Catalog {
	return &Catalog{pages: make(map[string]Page)}
}

func (catalog *Catalog) Register(page Page) error {
	if page.ID == "" || page.Label == "" || page.Path == "" {
		return fmt.Errorf("page ID, label, and path are required")
	}
	key := string(page.Area) + ":" + page.ID
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if _, exists := catalog.pages[key]; exists {
		return fmt.Errorf("page %q is already registered in area %q", page.ID, page.Area)
	}
	catalog.pages[key] = page
	return nil
}

func (catalog *Catalog) Pages(area Area) []Page {
	catalog.mu.RLock()
	pages := make([]Page, 0, len(catalog.pages))
	for _, page := range catalog.pages {
		if page.Area == area {
			pages = append(pages, page)
		}
	}
	catalog.mu.RUnlock()
	sort.Slice(pages, func(i, j int) bool {
		if pages[i].Order == pages[j].Order {
			return pages[i].Label < pages[j].Label
		}
		return pages[i].Order < pages[j].Order
	})
	return pages
}

func (catalog *Catalog) Permissions() []string {
	catalog.mu.RLock()
	set := make(map[string]struct{})
	for _, page := range catalog.pages {
		if page.Permission != "" {
			set[page.Permission] = struct{}{}
		}
	}
	catalog.mu.RUnlock()
	permissions := make([]string, 0, len(set))
	for permission := range set {
		permissions = append(permissions, permission)
	}
	sort.Strings(permissions)
	return permissions
}
