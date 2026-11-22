//go:build linux

package sdnotify

import (
	"net"
	"os"
	"strings"
)

func send(state string) {
	sock := os.Getenv("NOTIFY_SOCKET")
	if sock == "" {
		return
	}
	if strings.HasPrefix(sock, "@") {
		sock = "\x00" + sock[1:]
	}
	conn, err := net.Dial("unixgram", sock)
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = conn.Write([]byte(state))
}

// Ready tells systemd the service is up.
func Ready() { send("READY=1") }

// Watchdog pings the systemd watchdog.
func Watchdog() { send("WATCHDOG=1") }

// Stopping tells systemd the process is shutting down.
func Stopping() { send("STOPPING=1") }
