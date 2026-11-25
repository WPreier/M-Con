package smtp

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"mcon/internal/message"
	"mcon/internal/provider"
)

func TestSinkCodes(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go serveSink(ln)

	client := &Client{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, TLSMode: "none", Timeout: 2 * time.Second, HELO: "mcon.test"}
	raw := []byte("Subject: hi\r\n\r\nhello\r\n")
	cases := []struct {
		to   string
		perm bool
		temp bool
	}{
		{"ok@example.com", false, false},
		{"temp@example.com", false, true},
		{"perm@example.com", true, false},
	}
	for _, tc := range cases {
		_, err := client.Send(context.Background(), message.Outbound{From: "noreply@example.com", To: []string{tc.to}, Raw: raw, MessageID: "<1@example.com>"})
		if tc.perm {
			if !provider.IsPermanent(err) {
				t.Fatalf("%s want permanent, got %v", tc.to, err)
			}
			continue
		}
		if tc.temp {
			if !provider.IsTransient(err) {
				t.Fatalf("%s want transient, got %v", tc.to, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s %v", tc.to, err)
		}
	}
}

func TestPlaintextRemoteRejected(t *testing.T) {
	c := &Client{Host: "203.0.113.10", Port: 25, TLSMode: "none"}
	_, err := c.Send(context.Background(), message.Outbound{From: "a@b.c", To: []string{"c@d.e"}, Raw: []byte("x\r\n")})
	if !provider.IsPermanent(err) {
		t.Fatalf("got %v", err)
	}
}

func TestTLSConfigInsecureSkipVerify(t *testing.T) {
	c := &Client{Host: "smtp.internal.example", InsecureSkipVerify: true}
	cfg := c.tlsConfig()
	if !cfg.InsecureSkipVerify || cfg.ServerName != "smtp.internal.example" {
		t.Fatalf("%+v", cfg)
	}
	c2 := &Client{Host: "smtp.example.com"}
	if c2.tlsConfig().InsecureSkipVerify {
		t.Fatal("default must verify certificates")
	}
}

func serveSink(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func(conn net.Conn) {
			defer conn.Close()
			r := bufio.NewReader(conn)
			w := bufio.NewWriter(conn)
			_, _ = w.WriteString("220 sink ready\r\n")
			_ = w.Flush()
			var badCode int
			for {
				line, err := r.ReadString('\n')
				if err != nil {
					return
				}
				line = strings.TrimRight(line, "\r\n")
				upper := strings.ToUpper(line)
				switch {
				case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
					_, _ = w.WriteString("250-sink\r\n250 OK\r\n")
				case strings.HasPrefix(upper, "MAIL"):
					_, _ = w.WriteString("250 OK\r\n")
				case strings.HasPrefix(upper, "RCPT"):
					code := 250
					if strings.Contains(line, "temp@") {
						code = 450
						badCode = 450
					}
					if strings.Contains(line, "perm@") {
						code = 550
						badCode = 550
					}
					_, _ = w.WriteString(itoa(code) + " OK\r\n")
				case strings.HasPrefix(upper, "DATA"):
					_, _ = w.WriteString("354 go ahead\r\n")
					_ = w.Flush()
					for {
						l, err := r.ReadString('\n')
						if err != nil {
							return
						}
						if strings.TrimRight(l, "\r\n") == "." {
							break
						}
					}
					code := 250
					if badCode != 0 {
						code = badCode
					}
					_, _ = w.WriteString(itoa(code) + " done\r\n")
				case strings.HasPrefix(upper, "QUIT"):
					_, _ = w.WriteString("221 bye\r\n")
					_ = w.Flush()
					return
				default:
					_, _ = w.WriteString("250 OK\r\n")
				}
				_ = w.Flush()
			}
		}(conn)
	}
}

func itoa(n int) string {
	return strconvItoa(n)
}

func strconvItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
