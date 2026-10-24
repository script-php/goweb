package main

import (
	_ "embed"
	"strings"
)

// versionFile is embedded so the running application always reports the
// version of the source it was built from.
//
//go:embed version.txt
var versionFile string

func applicationVersion() string {
	return parseVersion(versionFile)
}

func parseVersion(contents string) string {
	version := strings.TrimSpace(contents)
	if version == "" {
		return "dev"
	}
	return version
}
