// Package mail sends application email using SMTP configuration that may be
// overridden at runtime through the database-backed admin settings.
package mail

import (
	"bufio"
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"script-php/goweb/platform/configuration"
)

type Message struct{ To, Subject, Text string }

type Sender interface {
	Send(context.Context, Message) error
}

type SMTP struct {
	db       *sql.DB
	defaults config.Email
}

func NewSMTP(db *sql.DB, defaults config.Email) *SMTP { return &SMTP{db: db, defaults: defaults} }

func (sender *SMTP) Send(ctx context.Context, message Message) error {
	cfg, err := sender.settings(ctx)
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		return fmt.Errorf("email delivery is disabled")
	}
	if _, err := mail.ParseAddress(message.To); err != nil {
		return fmt.Errorf("invalid recipient: %w", err)
	}
	from := (&mail.Address{Name: cfg.FromName, Address: cfg.FromAddress}).String()
	body := "From: " + from + "\r\nTo: " + message.To + "\r\nSubject: " + sanitizeHeader(message.Subject) +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + strings.ReplaceAll(message.Text, "\n", "\r\n")
	address := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	dialer := net.Dialer{Timeout: cfg.Timeout.Duration}
	var conn net.Conn
	if cfg.Encryption == "tls" {
		conn, err = tls.DialWithDialer(&dialer, "tcp", address, &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return fmt.Errorf("connect to SMTP server: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(cfg.Timeout.Duration))
	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return fmt.Errorf("start SMTP client: %w", err)
	}
	defer client.Close()
	if cfg.Encryption == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("SMTP server does not support STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("start SMTP TLS: %w", err)
		}
	}
	if cfg.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return fmt.Errorf("authenticate SMTP: %w", err)
		}
	}
	if err := client.Mail(cfg.FromAddress); err != nil {
		return err
	}
	if err := client.Rcpt(message.To); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	writer := bufio.NewWriter(w)
	if _, err = writer.WriteString(body); err == nil {
		err = writer.Flush()
	}
	if closeErr := w.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("send SMTP message: %w", err)
	}
	return client.Quit()
}

func (sender *SMTP) settings(ctx context.Context) (config.Email, error) {
	cfg := sender.defaults
	rows, err := sender.db.QueryContext(ctx, `SELECT setting_key, setting_value FROM settings WHERE setting_key LIKE 'mail.%'`)
	if err != nil {
		return cfg, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return cfg, err
		}
		switch key {
		case "mail.enabled":
			cfg.Enabled = value == "true"
		case "mail.host":
			cfg.Host = value
		case "mail.port":
			if port, e := strconv.Atoi(value); e == nil {
				cfg.Port = port
			}
		case "mail.username":
			cfg.Username = value
		case "mail.password":
			if value != "" {
				cfg.Password = value
			}
		case "mail.from_name":
			cfg.FromName = value
		case "mail.from_address":
			cfg.FromAddress = value
		case "mail.encryption":
			cfg.Encryption = value
		}
	}
	return cfg, rows.Err()
}

func sanitizeHeader(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "\r", ""), "\n", "")
}
