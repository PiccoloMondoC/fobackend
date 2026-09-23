// Package email provides the SMTP transport behind the provider-neutral
// email.Provider interface.
//
// focodebase/fobackend/internal/notification_services/email/smtp_provider.go
//
// GTM:
//
//	Layer: 2.3 Consumer Domain
//	Release Class: SPINE
//	Reason:
//	  SMTP is a concrete outbound transport usable with local Mailpit and
//	  SMTP-compatible production relays without coupling application code to
//	  a vendor.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve provider-neutral transport ownership.
//	Preserve TLS/STARTTLS/plain transport modes as explicitly configured.
//	Never send credentials over an unencrypted SMTP session.
//	Preserve context cancellation and deadline propagation.
//	Preserve opaque transport errors that do not disclose recipients,
//	message bodies, activation URLs, or credentials.
//	Block deployment if this file breaks SMTP delivery or notification safety.
package email

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
)

var (
	ErrSMTPHostRequired       = errors.New("email: SMTP host is required")
	ErrSMTPHostInvalid        = errors.New("email: SMTP host is invalid")
	ErrSMTPPortInvalid        = errors.New("email: SMTP port is invalid")
	ErrSMTPSecurityInvalid    = errors.New("email: SMTP security mode is invalid")
	ErrSMTPCredentialMismatch = errors.New("email: SMTP username and password must both be set or both be empty")
	ErrSMTPInsecureAuth       = errors.New("email: SMTP authentication requires encrypted transport")
	ErrSMTPMessageInvalid     = errors.New("email: SMTP message is invalid")
	ErrSMTPDeliveryFailed     = errors.New("email: SMTP delivery failed")
)

type SMTPConfig struct {
	Host     string
	Port     string
	Username string
	Password string
	Security string
}

type SMTPProvider struct {
	host     string
	port     string
	username string
	password string
	security string
}

var _ Provider = (*SMTPProvider)(nil)

func NewSMTPProvider(cfg SMTPConfig) (*SMTPProvider, error) {
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return nil, ErrSMTPHostRequired
	}
	if !validSMTPHost(host) {
		return nil, ErrSMTPHostInvalid
	}

	port := strings.TrimSpace(cfg.Port)
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 1 || portNum > 65535 {
		return nil, ErrSMTPPortInvalid
	}

	security := strings.ToLower(strings.TrimSpace(cfg.Security))
	switch security {
	case "none", "starttls", "tls":
	default:
		return nil, ErrSMTPSecurityInvalid
	}

	if (cfg.Username == "") != (cfg.Password == "") {
		return nil, ErrSMTPCredentialMismatch
	}
	if cfg.Username != "" && security == "none" {
		return nil, ErrSMTPInsecureAuth
	}

	return &SMTPProvider{
		host:     host,
		port:     port,
		username: cfg.Username,
		password: cfg.Password,
		security: security,
	}, nil
}

func (p *SMTPProvider) Send(ctx context.Context, message Message) error {
	if ctx == nil {
		return ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil {
		return ErrSMTPDeliveryFailed
	}

	from, to, payload, err := prepareSMTPMessage(message)
	if err != nil {
		return err
	}

	addr := net.JoinHostPort(p.host, p.port)
	var dialer net.Dialer

	rawConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return smtpFailure(ctx, "dial")
	}

	var conn net.Conn = rawConn
	if p.security == "tls" {
		tlsConn := tls.Client(rawConn, p.tlsConfig())
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = rawConn.Close()
			return smtpFailure(ctx, "tls handshake")
		}
		conn = tlsConn
	}

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	stopContextWatch := watchSMTPContext(ctx, conn)
	defer stopContextWatch()
	defer conn.Close()

	client, err := smtp.NewClient(conn, p.host)
	if err != nil {
		return smtpFailure(ctx, "session")
	}
	defer client.Close()

	if p.security == "starttls" {
		ok, _ := client.Extension("STARTTLS")
		if !ok {
			return smtpFailure(ctx, "starttls unavailable")
		}
		if err := client.StartTLS(p.tlsConfig()); err != nil {
			return smtpFailure(ctx, "starttls")
		}
	}

	if p.username != "" {
		ok, _ := client.Extension("AUTH")
		if !ok {
			return smtpFailure(ctx, "auth unavailable")
		}

		auth := smtp.PlainAuth("", p.username, p.password, p.host)
		if err := client.Auth(auth); err != nil {
			return smtpFailure(ctx, "auth")
		}
	}

	if err := client.Mail(from); err != nil {
		return smtpFailure(ctx, "mail from")
	}
	if err := client.Rcpt(to); err != nil {
		return smtpFailure(ctx, "rcpt to")
	}

	writer, err := client.Data()
	if err != nil {
		return smtpFailure(ctx, "data")
	}

	if _, err := writer.Write(payload); err != nil {
		_ = writer.Close()
		return smtpFailure(ctx, "write")
	}
	if err := writer.Close(); err != nil {
		return smtpFailure(ctx, "finalize")
	}
	if err := client.Quit(); err != nil {
		return smtpFailure(ctx, "quit")
	}

	return nil
}

func (p *SMTPProvider) tlsConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: p.host,
	}
}

func watchSMTPContext(ctx context.Context, conn net.Conn) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

func smtpFailure(ctx context.Context, stage string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("%w: %s", ErrSMTPDeliveryFailed, stage)
}

func prepareSMTPMessage(message Message) (fromEnvelope, toEnvelope string, payload []byte, err error) {
	if strings.ContainsAny(message.From, "\r\n") ||
		strings.ContainsAny(message.To, "\r\n") ||
		strings.ContainsAny(message.Subject, "\r\n") {
		return "", "", nil, ErrSMTPMessageInvalid
	}
	if message.From == "" || message.To == "" || message.Subject == "" || message.Body == "" {
		return "", "", nil, ErrSMTPMessageInvalid
	}

	fromAddr, err := mail.ParseAddress(message.From)
	if err != nil || fromAddr.Address == "" {
		return "", "", nil, ErrSMTPMessageInvalid
	}
	toAddr, err := mail.ParseAddress(message.To)
	if err != nil || toAddr.Address == "" {
		return "", "", nil, ErrSMTPMessageInvalid
	}

	var body strings.Builder
	qp := quotedprintable.NewWriter(&body)
	if _, err := qp.Write([]byte(normalizeSMTPBodyLineEndings(message.Body))); err != nil {
		return "", "", nil, ErrSMTPMessageInvalid
	}
	if err := qp.Close(); err != nil {
		return "", "", nil, ErrSMTPMessageInvalid
	}

	var b strings.Builder
	b.WriteString("From: " + fromAddr.String() + "\r\n")
	b.WriteString("To: " + toAddr.String() + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", message.Subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
	b.WriteString("\r\n")
	b.WriteString(body.String())

	return fromAddr.Address, toAddr.Address, []byte(b.String()), nil
}

func normalizeSMTPBodyLineEndings(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	return strings.ReplaceAll(body, "\n", "\r\n")
}

func validSMTPHost(host string) bool {
	if strings.Contains(host, "://") || strings.ContainsAny(host, "/ \t\r\n") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return true
	}
	if len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 ||
			strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') ||
				(r >= 'A' && r <= 'Z') ||
				(r >= '0' && r <= '9') ||
				r == '-' {
				continue
			}
			return false
		}
	}
	return true
}
