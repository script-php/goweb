// Package app contains only project-specific registration and behavior.
package app

import "script-php/goweb/platform"

func RegisterPages(pages *platform.Pages) error {
	if err := pages.Template("GET /{$}", platform.Page{ID: "home", Label: "Home", Path: "/", Description: "Website home page", Navigation: true, Order: 10}, "home", "Home", nil); err != nil {
		return err
	}
	return pages.Template("GET /about", platform.Page{ID: "about", Label: "About", Path: "/about", Description: "About the website", Navigation: true, Order: 20}, "about", "About", nil)
}

func RegisterAdmin(admin *platform.Admin) error {
	// Project-specific admin routes, widgets, user-form sections, and overrides
	// are registered here. The starter intentionally ships with no app-specific
	// admin additions.
	return nil
}
