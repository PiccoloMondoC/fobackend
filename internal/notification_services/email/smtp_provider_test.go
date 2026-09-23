package email

import (
	"context"
	"net/mail"
	"strings"
	"testing"
)

func TestNewSMTPProviderValidation(t *testing.T) {
	if _, err := NewSMTPProvider(SMTPConfig{
		Host: "mailpit", Port: "1025", Security: "none",
	}); err != nil {
		t.Fatalf("valid Mailpit config rejected: %v", err)
	}

	if _, err := NewSMTPProvider(SMTPConfig{
		Host: "mailpit", Port: "1025", Security: "none",
		Username: "u", Password: "p",
	}); err != ErrSMTPInsecureAuth {
		t.Fatalf("expected ErrSMTPInsecureAuth, got %v", err)
	}

	if _, err := NewSMTPProvider(SMTPConfig{
		Host: "mailpit", Port: "1025", Security: "starttls",
		Username: "u", Password: "p",
	}); err != nil {
		t.Fatalf("encrypted auth config rejected: %v", err)
	}
}

func TestPrepareSMTPMessageUsesBareEnvelopeSender(t *testing.T) {
	from, to, payload, err := prepareSMTPMessage(Message{
		From:    "Sagrenti <no-reply@sagrenti.local>",
		To:      "user@example.com",
		Subject: "Activate Your Account",
		Body:    "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if from != "no-reply@sagrenti.local" {
		t.Fatalf("unexpected envelope sender: %q", from)
	}

	if to != "user@example.com" {
		t.Fatalf("unexpected envelope recipient: %q", to)
	}

	payloadText := string(payload)

	fromHeader := ""
	for _, line := range strings.Split(payloadText, "\r\n") {
		if strings.HasPrefix(line, "From: ") {
			fromHeader = strings.TrimPrefix(line, "From: ")
			break
		}
	}

	if fromHeader == "" {
		t.Fatalf("From header missing: %s", payloadText)
	}

	headerAddr, err := mail.ParseAddress(fromHeader)
	if err != nil {
		t.Fatalf("From header is not a valid mail address: %v; header=%q", err, fromHeader)
	}

	if headerAddr.Name != "Sagrenti" {
		t.Fatalf("display name not preserved: got %q", headerAddr.Name)
	}

	if headerAddr.Address != "no-reply@sagrenti.local" {
		t.Fatalf("unexpected From header address: %q", headerAddr.Address)
	}
}

func TestPrepareSMTPMessageRejectsHeaderInjection(t *testing.T) {
	_, _, _, err := prepareSMTPMessage(Message{
		From:    "a@example.com",
		To:      "b@example.com",
		Subject: "hello\r\nBcc: attacker@example.com",
		Body:    "body",
	})
	if err != ErrSMTPMessageInvalid {
		t.Fatalf("expected ErrSMTPMessageInvalid, got %v", err)
	}
}

func TestSMTPProviderSendHonorsCanceledContext(t *testing.T) {
	p, err := NewSMTPProvider(SMTPConfig{
		Host: "mailpit", Port: "1025", Security: "none",
	})
	if err != nil {
		t.Fatalf("construct provider: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = p.Send(ctx, Message{
		From:    "no-reply@sagrenti.local",
		To:      "user@example.com",
		Subject: "subject",
		Body:    "body",
	})
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
