//go:build !linux

package service

import (
	"errors"

	"script-php/goweb/platform/configuration"
)

func Register(config.Service, string) error {
	return errors.New("service registration is supported only on Linux systems using systemd")
}

func Unregister(config.Service) error {
	return errors.New("service registration is supported only on Linux systems using systemd")
}
