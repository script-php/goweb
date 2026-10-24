//go:build linux

package service

import (
	"strings"
	"testing"

	"script-php/goweb/platform/configuration"
)

func TestBuildUnit(t *testing.T) {
	settings := config.Service{
		Name:        "example-site",
		Description: "Example website",
		User:        "www-data",
		Group:       "www-data",
	}
	unit, err := buildUnit(settings, "/opt/example site/website", "/opt/example site/config.json")
	if err != nil {
		t.Fatalf("buildUnit() error = %v", err)
	}

	text := string(unit)
	for _, expected := range []string{
		managedMarker,
		"Description=Example website",
		"User=www-data",
		"Group=www-data",
		`ExecStart="/opt/example site/website" --config "/opt/example site/config.json"`,
		`ReadWritePaths="/opt/example site"`,
		"ProtectSystem=strict",
		"ProtectKernelModules=true",
		"CapabilityBoundingSet=",
		"UMask=0027",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("unit does not contain %q:\n%s", expected, text)
		}
	}
}

func TestBuildUnitRejectsNewlines(t *testing.T) {
	_, err := buildUnit(config.Service{Description: "unsafe\nDirective=true"}, "/opt/site", "/opt/config.json")
	if err == nil {
		t.Fatal("buildUnit() accepted a newline in the description")
	}
}

func TestValidServiceName(t *testing.T) {
	for _, name := range []string{"website", "example-site", "site@example"} {
		if !validServiceName(name) {
			t.Errorf("validServiceName(%q) = false", name)
		}
	}
	for _, name := range []string{"", "../website", "site/name", "site name"} {
		if validServiceName(name) {
			t.Errorf("validServiceName(%q) = true", name)
		}
	}
}
