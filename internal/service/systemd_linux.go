//go:build linux

package service

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"script-php/goweb/platform/configuration"
)

const (
	systemdDirectory = "/etc/systemd/system"
	managedMarker    = "# Managed by the website binary --register command."
)

func Register(settings config.Service, configPath string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	name := Name(settings)
	if !validServiceName(name) {
		return fmt.Errorf("invalid service name %q", name)
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	configPath, err = filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("resolve configuration path: %w", err)
	}

	unitPath := filepath.Join(systemdDirectory, name+".service")
	if err := refuseUnmanagedFile(unitPath); err != nil {
		return err
	}
	unit, err := buildUnit(settings, executable, configPath)
	if err != nil {
		return err
	}
	if err := writeAtomically(unitPath, unit); err != nil {
		return fmt.Errorf("write unit file %s: %w", unitPath, err)
	}
	if err := systemctl("daemon-reload"); err != nil {
		return err
	}
	if err := systemctl("enable", name+".service"); err != nil {
		return err
	}
	return nil
}

func Unregister(settings config.Service) error {
	if err := requireRoot(); err != nil {
		return err
	}

	name := Name(settings)
	if !validServiceName(name) {
		return fmt.Errorf("invalid service name %q", name)
	}
	unitName := name + ".service"
	unitPath := filepath.Join(systemdDirectory, unitName)

	contents, err := os.ReadFile(unitPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read unit file %s: %w", unitPath, err)
	}
	if !bytes.Contains(contents, []byte(managedMarker)) {
		return fmt.Errorf("refusing to remove unmanaged unit file %s", unitPath)
	}

	if err := systemctl("disable", "--now", unitName); err != nil {
		return err
	}
	if err := os.Remove(unitPath); err != nil {
		return fmt.Errorf("remove unit file %s: %w", unitPath, err)
	}
	if err := systemctl("daemon-reload"); err != nil {
		return err
	}
	return nil
}

func requireRoot() error {
	if os.Geteuid() != 0 {
		return errors.New("root privileges are required; run the command with sudo")
	}
	return nil
}

func buildUnit(settings config.Service, executable, configPath string) ([]byte, error) {
	for label, value := range map[string]string{
		"description": settings.Description,
		"user":        settings.User,
		"group":       settings.Group,
	} {
		if strings.ContainsAny(value, "\r\n\x00") {
			return nil, fmt.Errorf("service %s cannot contain line breaks or null bytes", label)
		}
	}

	description := settings.Description
	if description == "" {
		description = Name(settings)
	}
	workingDirectory := filepath.Dir(executable)

	var unit strings.Builder
	unit.WriteString(managedMarker + "\n")
	unit.WriteString("[Unit]\n")
	unit.WriteString("Description=" + description + "\n")
	unit.WriteString("Wants=network-online.target\n")
	unit.WriteString("After=network-online.target\n")
	unit.WriteString("StartLimitIntervalSec=60s\n")
	unit.WriteString("StartLimitBurst=5\n\n")
	unit.WriteString("[Service]\n")
	unit.WriteString("Type=simple\n")
	if settings.User != "" {
		unit.WriteString("User=" + settings.User + "\n")
	}
	if settings.Group != "" {
		unit.WriteString("Group=" + settings.Group + "\n")
	}
	unit.WriteString("WorkingDirectory=" + systemdQuote(workingDirectory) + "\n")
	unit.WriteString("ExecStart=" + systemdQuote(executable) + " --config " + systemdQuote(configPath) + "\n")
	unit.WriteString("Restart=on-failure\n")
	unit.WriteString("RestartSec=5s\n")
	unit.WriteString("TimeoutStopSec=30s\n")
	unit.WriteString("UMask=0027\n")
	unit.WriteString("NoNewPrivileges=true\n")
	unit.WriteString("PrivateTmp=true\n")
	unit.WriteString("PrivateDevices=true\n")
	unit.WriteString("ProtectSystem=strict\n")
	unit.WriteString("ProtectHome=read-only\n")
	unit.WriteString("ReadWritePaths=" + systemdQuote(workingDirectory) + "\n")
	unit.WriteString("ProtectKernelTunables=true\n")
	unit.WriteString("ProtectKernelModules=true\n")
	unit.WriteString("ProtectKernelLogs=true\n")
	unit.WriteString("ProtectControlGroups=true\n")
	unit.WriteString("ProtectClock=true\n")
	unit.WriteString("ProtectHostname=true\n")
	unit.WriteString("RestrictSUIDSGID=true\n")
	unit.WriteString("LockPersonality=true\n")
	unit.WriteString("MemoryDenyWriteExecute=true\n")
	unit.WriteString("RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6\n")
	unit.WriteString("SystemCallArchitectures=native\n")
	unit.WriteString("CapabilityBoundingSet=\n")
	unit.WriteString("AmbientCapabilities=\n\n")
	unit.WriteString("[Install]\n")
	unit.WriteString("WantedBy=multi-user.target\n")
	return []byte(unit.String()), nil
}

func systemdQuote(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}

func refuseUnmanagedFile(path string) error {
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect existing unit file %s: %w", path, err)
	}
	if !bytes.Contains(contents, []byte(managedMarker)) {
		return fmt.Errorf("refusing to overwrite unmanaged unit file %s", path)
	}
	return nil
}

func writeAtomically(path string, contents []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".website-service-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func systemctl(arguments ...string) error {
	command := exec.Command("systemctl", arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("systemctl %s: %s", strings.Join(arguments, " "), message)
	}
	return nil
}
