package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

func readNewPassword() (string, error) {
	fileDescriptor := int(os.Stdin.Fd())
	if !term.IsTerminal(fileDescriptor) {
		return "", errors.New("--create-admin requires an interactive terminal for password entry")
	}
	fmt.Fprint(os.Stderr, "New administrator password: ")
	password, err := term.ReadPassword(fileDescriptor)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	fmt.Fprint(os.Stderr, "Confirm password: ")
	confirmation, err := term.ReadPassword(fileDescriptor)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password confirmation: %w", err)
	}
	if string(password) != string(confirmation) {
		return "", errors.New("passwords do not match")
	}
	return strings.TrimSpace(string(password)), nil
}
