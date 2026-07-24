package email

import (
	"bytes"
	"context"
	"fmt"
	"net/smtp"

	"github.com/google/uuid"
)

// SMTPConfig holds connection settings for SMTP email dispatch.
type SMTPConfig struct {
	Host        string
	Port        int
	Username    string
	Password    string
	FromAddress string
	FromName    string
}

// SMTPProvider implements Provider using Go stdlib net/smtp.
type SMTPProvider struct {
	config SMTPConfig
}

// NewSMTPProvider creates a new SMTPProvider with the given configuration.
func NewSMTPProvider(cfg SMTPConfig) *SMTPProvider {
	if cfg.Port == 0 {
		cfg.Port = 587
	}
	return &SMTPProvider{config: cfg}
}

// Send formats the MIME payload and delivers the email over SMTP.
func (s *SMTPProvider) Send(ctx context.Context, msg *EmailMessage) (string, error) {
	from := msg.From
	if from == "" {
		from = s.config.FromAddress
	}
	if from == "" {
		return "", fmt.Errorf("from address is required")
	}

	fromName := msg.FromName
	if fromName == "" {
		fromName = s.config.FromName
	}

	providerMsgID := msg.ID
	if providerMsgID == "" {
		providerMsgID = uuid.New().String()
	}

	mimePayload := BuildMIMEPayload(from, fromName, msg, providerMsgID)

	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)

	var auth smtp.Auth
	if s.config.Username != "" {
		auth = smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)
	}

	// Connect and send
	err := sendMail(addr, auth, from, msg.To, mimePayload.Bytes())
	if err != nil {
		return "", fmt.Errorf("smtp send error to %v: %w", msg.To, err)
	}

	return providerMsgID, nil
}

// sendMail wraps smtp.SendMail for testability.
var sendMail = func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
	return smtp.SendMail(addr, a, from, to, msg)
}

// BuildMIMEPayload builds a standard MIME multipart/alternative or text/plain body.
func BuildMIMEPayload(from, fromName string, msg *EmailMessage, msgID string) *bytes.Buffer {
	buf := new(bytes.Buffer)

	// Format From header
	if fromName != "" {
		fmt.Fprintf(buf, "From: %s <%s>\r\n", fromName, from)
	} else {
		fmt.Fprintf(buf, "From: %s\r\n", from)
	}

	// Format To header
	if len(msg.To) > 0 {
		fmt.Fprintf(buf, "To: %s\r\n", msg.To[0])
	}

	if msg.ReplyTo != "" {
		fmt.Fprintf(buf, "Reply-To: %s\r\n", msg.ReplyTo)
	}

	fmt.Fprintf(buf, "Subject: %s\r\n", msg.Subject)
	fmt.Fprintf(buf, "Message-ID: <%s@pergo.dev>\r\n", msgID)
	fmt.Fprintf(buf, "MIME-Version: 1.0\r\n")

	// Custom Headers
	for k, v := range msg.Headers {
		fmt.Fprintf(buf, "%s: %s\r\n", k, v)
	}

	boundSuffix := msgID
	if len(boundSuffix) > 8 {
		boundSuffix = boundSuffix[:8]
	}
	boundary := "---=_PerGo_Boundary_" + boundSuffix

	if msg.HTMLBody != "" {
		fmt.Fprintf(buf, "Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", boundary)
		fmt.Fprintf(buf, "--%s\r\n", boundary)
		fmt.Fprintf(buf, "Content-Type: text/plain; charset=UTF-8\r\n\r\n")
		buf.WriteString(msg.TextBody)
		buf.WriteString("\r\n\r\n")

		fmt.Fprintf(buf, "--%s\r\n", boundary)
		fmt.Fprintf(buf, "Content-Type: text/html; charset=UTF-8\r\n\r\n")
		buf.WriteString(msg.HTMLBody)
		buf.WriteString("\r\n\r\n")
		fmt.Fprintf(buf, "--%s--\r\n", boundary)
	} else {
		fmt.Fprintf(buf, "Content-Type: text/plain; charset=UTF-8\r\n\r\n")
		buf.WriteString(msg.TextBody)
		buf.WriteString("\r\n")
	}

	return buf
}
