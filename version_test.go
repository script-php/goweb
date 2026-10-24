package main

import "testing"

func TestApplicationVersion(t *testing.T) {
	if got := applicationVersion(); got == "" || got == "dev" {
		t.Fatalf("applicationVersion() = %q, want embedded release version", got)
	}
}

func TestParseVersion(t *testing.T) {
	if got := parseVersion("  1.2.3\n"); got != "1.2.3" {
		t.Fatalf("parseVersion() = %q, want %q", got, "1.2.3")
	}
	if got := parseVersion(" \n"); got != "dev" {
		t.Fatalf("parseVersion() = %q, want %q", got, "dev")
	}
}
