// This file implements SMTP email sending for CatalogRO notifications.
//
// Uses Go's standard library net/smtp — no external dependency needed.
// Schools can configure their own institutional SMTP server, ensuring
// full GDPR compliance (data stays in the EU, on the school's infra).
//
// Configuration via environment variables:
//
//	SMTP_HOST=smtp.scoala-rebreanu.ro
//	SMTP_PORT=587
//	SMTP_USERNAME=catalog@scoala-rebreanu.ro
//	SMTP_PASSWORD=<secret>
//	SMTP_FROM=catalog@scoala-rebreanu.ro
//	SMTP_TLS=starttls       (starttls | tls | none)
//
// If SMTP_HOST is not set, email sending is disabled (logged only).
package notification

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"os"
	"strings"
)

// SMTPConfig holds the SMTP connection settings.
type SMTPConfig struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string
	TLS      string // "starttls", "tls", or "none"
}

// SMTPSender sends emails via SMTP using Go's standard library.
type SMTPSender struct {
	config SMTPConfig
	logger *slog.Logger
}

// LoadSMTPConfig reads SMTP settings from environment variables.
// Returns nil if SMTP_HOST is not configured (email disabled).
func LoadSMTPConfig() *SMTPConfig {
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		return nil
	}

	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "587" // default SMTP submission port
	}

	// SMTP_FROM is required when SMTP_HOST is configured — fail fast if missing.
	from := os.Getenv("SMTP_FROM")
	if from == "" {
		panic("FATAL: SMTP_FROM must be set when SMTP_HOST is configured")
	}

	tlsMode := strings.ToLower(os.Getenv("SMTP_TLS"))
	if tlsMode == "" {
		tlsMode = "starttls" // default: STARTTLS (most common for port 587)
	}

	// Validate TLS mode — reject unknown values and block plaintext in production.
	switch tlsMode {
	case "starttls", "tls":
		// Valid and secure modes.
	case "none":
		if os.Getenv("ENV") == "production" {
			panic("FATAL: insecure SMTP_TLS=none is not allowed in production")
		}
		slog.Warn("SMTP_TLS=none — emails will be sent without encryption (development only)")
	default:
		panic(fmt.Sprintf("FATAL: invalid SMTP_TLS value %q; must be 'starttls', 'tls', or 'none'", tlsMode))
	}

	return &SMTPConfig{
		Host:     host,
		Port:     port,
		Username: os.Getenv("SMTP_USERNAME"),
		Password: os.Getenv("SMTP_PASSWORD"),
		From:     from,
		TLS:      tlsMode,
	}
}

// NewSMTPSender creates an SMTP sender with the given config.
// If config is nil, returns a no-op sender that only logs.
func NewSMTPSender(config *SMTPConfig, logger *slog.Logger) *SMTPSender {
	sender := &SMTPSender{logger: logger}
	if config != nil {
		sender.config = *config
	}
	return sender
}

// IsEnabled returns true if SMTP is configured.
func (s *SMTPSender) IsEnabled() bool {
	return s.config.Host != ""
}

// SendEmail sends a plain-text email to one or more recipients.
// If SMTP is not configured, logs the email and returns nil.
func (s *SMTPSender) SendEmail(to []string, subject, body string) error {
	if !s.IsEnabled() {
		s.logger.Info("email sending disabled (SMTP not configured)",
			"to", to, "subject", subject)
		return nil
	}

	// Build the email message in RFC 5322 format.
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		s.config.From,
		strings.Join(to, ", "),
		subject,
		body,
	)

	addr := net.JoinHostPort(s.config.Host, s.config.Port)

	var auth smtp.Auth
	if s.config.Username != "" {
		auth = smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)
	}

	switch s.config.TLS {
	case "tls":
		// Direct TLS connection (port 465).
		return s.sendWithTLS(addr, auth, to, []byte(msg))
	case "none":
		// Plain SMTP (no encryption — development only).
		return smtp.SendMail(addr, auth, s.config.From, to, []byte(msg))
	default:
		// STARTTLS (port 587, default) — Go's smtp.SendMail handles STARTTLS
		// automatically when the server advertises it.
		return smtp.SendMail(addr, auth, s.config.From, to, []byte(msg))
	}
}

// sendWithTLS sends email over an implicit TLS connection (port 465).
// Go's smtp.SendMail only supports STARTTLS, not implicit TLS.
func (s *SMTPSender) sendWithTLS(addr string, auth smtp.Auth, to []string, msg []byte) error {
	tlsConfig := &tls.Config{
		ServerName: s.config.Host,
		MinVersion: tls.VersionTLS12,
	}

	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("TLS dial %s: %w", addr, err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.config.Host)
	if err != nil {
		return fmt.Errorf("create SMTP client: %w", err)
	}
	defer client.Close()

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP auth: %w", err)
		}
	}

	if err := client.Mail(s.config.From); err != nil {
		return fmt.Errorf("SMTP MAIL FROM: %w", err)
	}

	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("SMTP RCPT TO %s: %w", recipient, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA: %w", err)
	}

	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("write message: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("close data writer: %w", err)
	}

	return client.Quit()
}
