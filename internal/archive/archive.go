// Package archive stores the RFC822 bytes that were accepted.
package archive

import (
	"os"
	"path/filepath"
	"time"
)

// Archive writes .eml files under a root directory.
type Archive struct {
	Dir string
}

// Write stores raw under YYYY/MM/<id>.eml and returns the final path.
func (a Archive) Write(id string, raw []byte, now time.Time) (string, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	dir := filepath.Join(a.Dir, now.Format("2006"), now.Format("01"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	final := filepath.Join(dir, id+".eml")
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return final, nil
}

// Read returns a previously stored message.
func (a Archive) Read(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// Remove deletes a file that was written for a rolled-back accept.
func (a Archive) Remove(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}
