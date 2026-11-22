package version

import (
	"fmt"
	"strconv"
	"strings"
)

// Bump raises a major.minor.patch version.
// kind is "major", "minor", or "patch".
func Bump(current, kind string) (string, error) {
	major, minor, patch, err := Parse(current)
	if err != nil {
		return "", err
	}
	switch kind {
	case "major":
		major++
		minor = 0
		patch = 0
	case "minor":
		minor++
		patch = 0
	case "patch":
		patch++
	default:
		return "", fmt.Errorf("unknown bump kind %q", kind)
	}
	return fmt.Sprintf("%d.%d.%d", major, minor, patch), nil
}

// Parse reads a strict major.minor.patch version without a leading v.
func Parse(v string) (major, minor, patch int, err error) {
	v = strings.TrimSpace(v)
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("invalid semver %q", v)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, convErr := strconv.Atoi(p)
		if convErr != nil || n < 0 || (len(p) > 1 && p[0] == '0') {
			return 0, 0, 0, fmt.Errorf("invalid semver %q", v)
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], nil
}

// RequiredBump inspects a Conventional Commit message.
// An empty kind means no VERSION bump is required.
func RequiredBump(message string) string {
	msg := stripComments(message)
	subject := firstLine(msg)
	kind := "none"
	switch {
	case strings.Contains(msg, "BREAKING CHANGE"):
		kind = "major"
	case breakingSubject(subject):
		kind = "major"
	case hasType(subject, "feat"):
		kind = "minor"
	case hasType(subject, "fix"):
		kind = "patch"
	}
	if kind == "none" {
		return ""
	}
	return kind
}

func stripComments(message string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.ReplaceAll(message, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func firstLine(message string) string {
	message = strings.TrimSpace(message)
	if i := strings.IndexByte(message, '\n'); i >= 0 {
		return strings.TrimSpace(message[:i])
	}
	return message
}

func breakingSubject(subject string) bool {
	// type! or type(scope)!:
	rest := subject
	if i := strings.IndexByte(rest, ':'); i < 0 {
		return false
	} else {
		head := rest[:i]
		return strings.HasSuffix(head, "!")
	}
}

func hasType(subject, typ string) bool {
	if subject == typ {
		return false
	}
	if strings.HasPrefix(subject, typ+":") {
		return true
	}
	if strings.HasPrefix(subject, typ+"(") {
		if i := strings.IndexByte(subject, ')'); i > 0 && strings.HasPrefix(subject[i:], "):") {
			return true
		}
	}
	return false
}
