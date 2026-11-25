// Package smtp delivers mail to an SMTP server.
package smtp

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"mcon/internal/message"
	"mcon/internal/provider"
)

// Client is an SMTP delivery adapter.
type Client struct {
	Host                  string
	Port                  int
	TLSMode               string
	InsecureSkipVerify    bool
	Username              string
	Password              string
	HELO                  string
	Timeout               time.Duration
}

func (c *Client) tlsConfig() *tls.Config {
	return &tls.Config{
		ServerName:         c.Host,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: c.InsecureSkipVerify, //nolint:gosec // optional for internal/expired SMTP certs
	}
}

// Send submits the archived RFC822 bytes.
func (c *Client) Send(ctx context.Context, msg message.Outbound) (provider.Result, error) {
	if c.TLSMode == "none" && !loopback(c.Host) {
		return provider.Result{}, provider.Permanent("smtp", "plaintext SMTP is only allowed to a loopback host", 0)
	}
	if len(msg.Raw) == 0 {
		return provider.Result{}, provider.Permanent("smtp", "missing rfc822 body", 0)
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	d := &net.Dialer{}
	var conn net.Conn
	var err error
	if c.TLSMode == "tls" {
		conn, err = tls.DialWithDialer(&net.Dialer{Timeout: timeout}, "tcp", addr, c.tlsConfig())
	} else {
		conn, err = d.DialContext(dialCtx, "tcp", addr)
	}
	if err != nil {
		return provider.Result{}, provider.Transient("smtp", err.Error(), 0)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	if code, line, err := readReply(r); err != nil || code >= 400 {
		return provider.Result{}, classify("smtp", code, line, err)
	}
	helo := c.HELO
	if helo == "" {
		helo = "mcon.local"
	}
	if err := cmd(w, r, "EHLO "+helo); err != nil {
		return provider.Result{}, err
	}
	if c.TLSMode == "starttls" {
		if err := cmd(w, r, "STARTTLS"); err != nil {
			return provider.Result{}, err
		}
		tlsConn := tls.Client(conn, c.tlsConfig())
		if err := tlsConn.Handshake(); err != nil {
			return provider.Result{}, provider.Transient("smtp", err.Error(), 0)
		}
		_ = tlsConn.SetDeadline(time.Now().Add(timeout))
		conn = tlsConn
		r = bufio.NewReader(conn)
		w = bufio.NewWriter(conn)
		if err := cmd(w, r, "EHLO "+helo); err != nil {
			return provider.Result{}, err
		}
	}
	if c.Username != "" {
		plain := base64.StdEncoding.EncodeToString([]byte("\x00" + c.Username + "\x00" + c.Password))
		if err := cmd(w, r, "AUTH PLAIN "+plain); err != nil {
			return provider.Result{}, err
		}
	}
	if err := cmd(w, r, "MAIL FROM:<"+msg.From+">"); err != nil {
		return provider.Result{}, err
	}
	rcpts := append(append(append([]string{}, msg.To...), msg.Cc...), msg.Bcc...)
	for _, rcpt := range rcpts {
		if err := cmd(w, r, "RCPT TO:<"+rcpt+">"); err != nil {
			return provider.Result{}, err
		}
	}
	if err := writeData(w, r, msg.Raw); err != nil {
		return provider.Result{}, err
	}
	_ = cmd(w, r, "QUIT")
	return provider.Result{ProviderMessageID: msg.MessageID}, nil
}

func cmd(w *bufio.Writer, r *bufio.Reader, line string) error {
	if _, err := w.WriteString(line + "\r\n"); err != nil {
		return provider.Transient("smtp", err.Error(), 0)
	}
	if err := w.Flush(); err != nil {
		return provider.Transient("smtp", err.Error(), 0)
	}
	code, text, err := readReply(r)
	if err != nil {
		return provider.Transient("smtp", err.Error(), 0)
	}
	return classifyCode(code, text)
}

func writeData(w *bufio.Writer, r *bufio.Reader, raw []byte) error {
	if _, err := w.WriteString("DATA\r\n"); err != nil {
		return provider.Transient("smtp", err.Error(), 0)
	}
	if err := w.Flush(); err != nil {
		return provider.Transient("smtp", err.Error(), 0)
	}
	code, text, err := readReply(r)
	if err != nil {
		return provider.Transient("smtp", err.Error(), 0)
	}
	if err := classifyCode(code, text); err != nil {
		return err
	}
	if len(raw) < 2 || raw[len(raw)-2] != '\r' || raw[len(raw)-1] != '\n' {
		raw = append(append([]byte{}, raw...), '\r', '\n')
	}
	lines := strings.Split(string(raw), "\r\n")
	for i, line := range lines {
		if i == len(lines)-1 && line == "" {
			break
		}
		if strings.HasPrefix(line, ".") {
			line = "." + line
		}
		if _, err := w.WriteString(line + "\r\n"); err != nil {
			return provider.Transient("smtp", err.Error(), 0)
		}
	}
	if _, err := w.WriteString(".\r\n"); err != nil {
		return provider.Transient("smtp", err.Error(), 0)
	}
	if err := w.Flush(); err != nil {
		return provider.Transient("smtp", err.Error(), 0)
	}
	code, text, err = readReply(r)
	if err != nil {
		return provider.Transient("smtp", err.Error(), 0)
	}
	return classifyCode(code, text)
}

func readReply(r *bufio.Reader) (int, string, error) {
	var code int
	var last string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return 0, "", err
		}
		line = strings.TrimRight(line, "\r\n")
		if len(line) < 3 {
			return 0, line, fmt.Errorf("short smtp reply %q", line)
		}
		n, err := strconv.Atoi(line[:3])
		if err != nil {
			return 0, line, err
		}
		code = n
		last = line
		if len(line) == 3 || line[3] == ' ' {
			return code, last, nil
		}
	}
}

func classifyCode(code int, text string) error {
	if code >= 200 && code < 400 {
		return nil
	}
	return classify("smtp", code, text, nil)
}

func classify(name string, code int, text string, err error) error {
	if err != nil && code == 0 {
		return provider.Transient(name, err.Error(), 0)
	}
	if code >= 500 {
		return provider.Permanent(name, text, code)
	}
	return provider.Transient(name, text, code)
}

func loopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
