package main

import (
	"fmt"
	"os"
	"path/filepath"

	"script-php/goweb/internal/service"
	"script-php/goweb/platform/configuration"
)

func checkDeployment(settings config.Service) error {
	if err := service.Validate(settings); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable directory: %w", err)
	}
	directory := filepath.Dir(executable)
	temporary, err := os.CreateTemp(directory, ".website-write-check-*")
	if err != nil {
		return fmt.Errorf("release directory %s is not writable: %w", directory, err)
	}
	name := temporary.Name()
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("clean deployment check file: %w", err)
	}
	return nil
}
