//go:build !linux

package logging

import (
	"errors"
	"io"
)

// OpenSyslog is unavailable outside Linux. The daemon logs that and continues.
func OpenSyslog(network, addr, tag, facility string) (io.WriteCloser, error) {
	return nil, errors.New("syslog is only supported on linux")
}
