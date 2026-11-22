//go:build linux

package logging

import (
	"io"
	"log/syslog"
)

// OpenSyslog dials a syslog server. Facility defaults to mail.
func OpenSyslog(network, addr, tag, facility string) (io.WriteCloser, error) {
	if tag == "" {
		tag = "mcon"
	}
	return syslog.Dial(network, addr, syslog.LOG_INFO|facilityPriority(facility), tag)
}

func facilityPriority(name string) syslog.Priority {
	switch name {
	case "user":
		return syslog.LOG_USER
	case "daemon":
		return syslog.LOG_DAEMON
	case "local0":
		return syslog.LOG_LOCAL0
	case "local1":
		return syslog.LOG_LOCAL1
	case "local2":
		return syslog.LOG_LOCAL2
	case "local3":
		return syslog.LOG_LOCAL3
	case "local4":
		return syslog.LOG_LOCAL4
	case "local5":
		return syslog.LOG_LOCAL5
	case "local6":
		return syslog.LOG_LOCAL6
	case "local7":
		return syslog.LOG_LOCAL7
	default:
		return syslog.LOG_MAIL
	}
}
