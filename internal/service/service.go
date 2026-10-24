// Package service registers the release binary with the host's service manager.
package service

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"script-php/goweb/platform/configuration"
)

var validName = regexp.MustCompile(`^[A-Za-z0-9_.@-]+$`)

func Name(settings config.Service) string {
	name := strings.TrimSuffix(settings.Name, ".service")
	if name != "" {
		return name
	}
	executable, err := os.Executable()
	if err != nil {
		return "website"
	}
	return strings.TrimSuffix(filepath.Base(executable), filepath.Ext(executable))
}

func validServiceName(name string) bool {
	return name != "" && name != "." && name != ".." && validName.MatchString(name)
}

func Validate(settings config.Service) error {
	name := Name(settings)
	if !validServiceName(name) {
		return fmt.Errorf("invalid service name %q", name)
	}
	for label, value := range map[string]string{"description": settings.Description, "user": settings.User, "group": settings.Group} {
		if strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("service %s cannot contain line breaks or null bytes", label)
		}
	}
	return nil
}
