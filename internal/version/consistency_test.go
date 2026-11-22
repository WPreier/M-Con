package version

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionFileMatchesChangelog(t *testing.T) {
	root := moduleRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(raw))
	if _, _, _, err := Parse(got); err != nil {
		t.Fatal(err)
	}
	if got != Version {
		t.Fatalf("VERSION file %s != package %s", got, Version)
	}
	log, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "## ["+got+"]") {
		t.Fatalf("CHANGELOG missing [%s]", got)
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
