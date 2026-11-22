// Package message validates submissions and builds RFC822 mail.
package message

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Attachment is one file supplied by the caller.
type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// Submission is the public JSON body after decoding.
type Submission struct {
	From        string
	To          []string
	Cc          []string
	Bcc         []string
	ReplyTo     string
	Subject     string
	Text        string
	HTML        string
	Attachments []Attachment
	Headers     map[string]string
	Tags        []string
	Metadata    map[string]any
	SpamHint    string
	Provider    string
}

// Outbound is a message ready to archive and send.
type Outbound struct {
	ID          string
	From        string
	To          []string
	Cc          []string
	Bcc         []string
	ReplyTo     string
	Subject     string
	Text        string
	HTML        string
	Headers     map[string]string
	Attachments []Attachment
	Raw         []byte
	MessageID   string
	Date        time.Time
}

// Limits come from the client configuration.
type Limits struct {
	MaxRecipients   int
	MaxMessageBytes int64
	MaxAttachments  int
	AllowedFrom     []string
}

// Validate checks size, addresses, and header injection.
func Validate(s Submission, lim Limits) error {
	if err := rejectControls(s.From, s.ReplyTo, s.Subject, s.Provider); err != nil {
		return err
	}
	for _, addr := range append(append(append([]string{}, s.To...), s.Cc...), s.Bcc...) {
		if err := checkAddr(addr); err != nil {
			return err
		}
	}
	if err := checkAddr(s.From); err != nil {
		return err
	}
	if s.ReplyTo != "" {
		if err := checkAddr(s.ReplyTo); err != nil {
			return err
		}
	}
	if strings.TrimSpace(s.Subject) == "" {
		return fmt.Errorf("subject is required")
	}
	if strings.TrimSpace(s.Text) == "" && strings.TrimSpace(s.HTML) == "" {
		return fmt.Errorf("text or html is required")
	}
	if len(s.To) == 0 {
		return fmt.Errorf("at least one to recipient is required")
	}
	n := len(s.To) + len(s.Cc) + len(s.Bcc)
	if lim.MaxRecipients > 0 && n > lim.MaxRecipients {
		return fmt.Errorf("too many recipients")
	}
	if lim.MaxAttachments > 0 && len(s.Attachments) > lim.MaxAttachments {
		return fmt.Errorf("too many attachments")
	}
	if len(s.Headers) > 40 {
		return fmt.Errorf("too many headers")
	}
	var size int64 = int64(len(s.Subject) + len(s.Text) + len(s.HTML))
	for name, value := range s.Headers {
		if err := rejectControls(name, value); err != nil {
			return err
		}
		if forbiddenHeader(name) {
			return fmt.Errorf("header %s is reserved", name)
		}
		if len(name) > 100 || len(value) > 998 {
			return fmt.Errorf("header %s is too long", name)
		}
	}
	for _, tag := range s.Tags {
		if err := rejectControls(tag); err != nil {
			return err
		}
		if len(tag) > 64 {
			return fmt.Errorf("tag is too long")
		}
	}
	if len(s.Tags) > 20 {
		return fmt.Errorf("too many tags")
	}
	for i, att := range s.Attachments {
		name := filepath.Base(att.Filename)
		if name == "." || name == "" || name == string(filepath.Separator) || strings.ContainsAny(name, `\/`) {
			return fmt.Errorf("attachment filename is invalid")
		}
		if err := rejectControls(name, att.ContentType); err != nil {
			return err
		}
		s.Attachments[i].Filename = name
		size += int64(len(att.Data))
	}
	if lim.MaxMessageBytes > 0 && size > lim.MaxMessageBytes {
		return fmt.Errorf("message exceeds max_message_bytes")
	}
	if len(lim.AllowedFrom) > 0 && !allowed(s.From, lim.AllowedFrom) {
		return fmt.Errorf("from is not allowed")
	}
	return nil
}

func allowed(from string, list []string) bool {
	for _, item := range list {
		if strings.EqualFold(strings.TrimSpace(item), from) {
			return true
		}
	}
	return false
}

func checkAddr(s string) error {
	if err := rejectControls(s); err != nil {
		return err
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || !strings.EqualFold(addr.Address, s) {
		return fmt.Errorf("invalid address %q", s)
	}
	return nil
}

func rejectControls(values ...string) error {
	for _, v := range values {
		if strings.ContainsAny(v, "\r\n\x00") {
			return fmt.Errorf("header injection")
		}
	}
	return nil
}

func forbiddenHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "from", "to", "cc", "bcc", "subject", "date", "message-id", "mime-version",
		"content-type", "content-transfer-encoding", "content-disposition", "sender",
		"reply-to", "return-path", "x-mcon-id":
		return true
	default:
		return false
	}
}

type canonAtt struct {
	Filename string `json:"filename"`
	SHA256   string `json:"sha256"`
}

type canonBody struct {
	From        string     `json:"from"`
	To          []string   `json:"to"`
	Cc          []string   `json:"cc"`
	Bcc         []string   `json:"bcc"`
	ReplyTo     string     `json:"reply_to"`
	Subject     string     `json:"subject"`
	Text        string     `json:"text"`
	HTML        string     `json:"html"`
	Attachments []canonAtt `json:"attachments"`
}

// BodyHash is a stable hash of the visible content, ignoring ids and custom headers.
func BodyHash(s Submission) string {
	body := canonBody{
		From:    strings.ToLower(s.From),
		To:      lowerSorted(s.To),
		Cc:      lowerSorted(s.Cc),
		Bcc:     lowerSorted(s.Bcc),
		ReplyTo: strings.ToLower(s.ReplyTo),
		Subject: s.Subject,
		Text:    s.Text,
		HTML:    s.HTML,
	}
	for _, att := range s.Attachments {
		sum := sha256.Sum256(att.Data)
		body.Attachments = append(body.Attachments, canonAtt{Filename: att.Filename, SHA256: hex.EncodeToString(sum[:])})
	}
	sort.Slice(body.Attachments, func(i, j int) bool {
		if body.Attachments[i].Filename == body.Attachments[j].Filename {
			return body.Attachments[i].SHA256 < body.Attachments[j].SHA256
		}
		return body.Attachments[i].Filename < body.Attachments[j].Filename
	})
	buf, _ := json.Marshal(body)
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:])
}

// ContentHash hashes the RFC822 bytes.
func ContentHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func lowerSorted(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strings.ToLower(s)
	}
	sort.Strings(out)
	return out
}

// MessageID builds a stable RFC message id for an internal id.
func MessageID(id, from string) string {
	domain := "mcon.local"
	if i := strings.LastIndex(from, "@"); i >= 0 && i < len(from)-1 {
		domain = from[i+1:]
	}
	return "<" + id + "@" + domain + ">"
}

// Build renders a hardened RFC822 message. Bcc stays off the header.
func Build(o Outbound) ([]byte, error) {
	if err := Validate(Submission{
		From: o.From, To: o.To, Cc: o.Cc, Bcc: o.Bcc, ReplyTo: o.ReplyTo,
		Subject: o.Subject, Text: o.Text, HTML: o.HTML, Attachments: o.Attachments, Headers: o.Headers,
	}, Limits{}); err != nil {
		return nil, err
	}
	now := o.Date
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var b bytes.Buffer
	write := func(k, v string) {
		fmt.Fprintf(&b, "%s: %s\r\n", k, v)
	}
	write("Date", now.UTC().Format(time.RFC1123Z))
	write("From", formatAddr(o.From))
	write("To", joinAddrs(o.To))
	if len(o.Cc) > 0 {
		write("Cc", joinAddrs(o.Cc))
	}
	if o.ReplyTo != "" {
		write("Reply-To", formatAddr(o.ReplyTo))
	}
	write("Subject", encodeSubject(o.Subject))
	mid := o.MessageID
	if mid == "" {
		mid = MessageID(o.ID, o.From)
	}
	write("Message-ID", mid)
	write("X-MCon-Id", o.ID)
	keys := make([]string, 0, len(o.Headers))
	for k := range o.Headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		write(k, o.Headers[k])
	}
	write("MIME-Version", "1.0")
	body, ctype, single := encodeBody(o)
	fmt.Fprintf(&b, "Content-Type: %s\r\n", ctype)
	if single {
		write("Content-Transfer-Encoding", "quoted-printable")
	}
	b.WriteString("\r\n")
	b.Write(body)
	return b.Bytes(), nil
}

func formatAddr(addr string) string {
	return (&mail.Address{Address: addr}).String()
}

func joinAddrs(list []string) string {
	parts := make([]string, len(list))
	for i, addr := range list {
		parts[i] = formatAddr(addr)
	}
	return strings.Join(parts, ", ")
}

func encodeSubject(s string) string {
	for _, r := range s {
		if r > 127 {
			return mime.QEncoding.Encode("utf-8", s)
		}
	}
	return s
}

func encodeBody(o Outbound) (body []byte, ctype string, single bool) {
	alt, altType, altSingle := alternative(o)
	if len(o.Attachments) == 0 {
		return alt, altType, altSingle
	}
	boundary := "mix-" + randToken()
	var b bytes.Buffer
	fmt.Fprintf(&b, "--%s\r\n", boundary)
	fmt.Fprintf(&b, "Content-Type: %s\r\n", altType)
	if altSingle {
		b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
	}
	b.WriteString("\r\n")
	b.Write(alt)
	if !bytes.HasSuffix(alt, []byte("\r\n")) {
		b.WriteString("\r\n")
	}
	for _, att := range o.Attachments {
		fmt.Fprintf(&b, "--%s\r\n", boundary)
		ct := att.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		fmt.Fprintf(&b, "Content-Type: %s\r\n", mime.FormatMediaType(ct, map[string]string{"name": att.Filename}))
		fmt.Fprintf(&b, "Content-Disposition: %s\r\n", mime.FormatMediaType("attachment", map[string]string{"filename": att.Filename}))
		b.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
		b.WriteString(wrapBase64(att.Data))
		b.WriteString("\r\n")
	}
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.Bytes(), fmt.Sprintf("multipart/mixed; boundary=%q", boundary), false
}

func alternative(o Outbound) (body []byte, ctype string, single bool) {
	hasText := strings.TrimSpace(o.Text) != ""
	hasHTML := strings.TrimSpace(o.HTML) != ""
	if hasText && hasHTML {
		boundary := "alt-" + randToken()
		var b bytes.Buffer
		fmt.Fprintf(&b, "--%s\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n%s", boundary, qp(o.Text))
		if !strings.HasSuffix(o.Text, "\n") {
			b.WriteString("\r\n")
		}
		fmt.Fprintf(&b, "--%s\r\nContent-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n%s\r\n--%s--\r\n", boundary, qp(o.HTML), boundary)
		return b.Bytes(), fmt.Sprintf("multipart/alternative; boundary=%q", boundary), false
	}
	if hasHTML {
		return []byte(qp(o.HTML)), "text/html; charset=utf-8", true
	}
	return []byte(qp(o.Text)), "text/plain; charset=utf-8", true
}

func qp(s string) string {
	var b bytes.Buffer
	w := quotedprintable.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.String()
}

func wrapBase64(data []byte) string {
	enc := base64.StdEncoding.EncodeToString(data)
	var b strings.Builder
	for len(enc) > 76 {
		b.WriteString(enc[:76])
		b.WriteString("\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc)
	return b.String()
}

func randToken() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

// AddressesOf returns every envelope recipient.
func AddressesOf(s Submission) []string {
	out := make([]string, 0, len(s.To)+len(s.Cc)+len(s.Bcc))
	out = append(out, s.To...)
	out = append(out, s.Cc...)
	out = append(out, s.Bcc...)
	return out
}
