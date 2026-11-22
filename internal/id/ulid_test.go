package id

import (
	"testing"
	"time"
)

func TestULIDShapeAndOrder(t *testing.T) {
	a := NewAt(time.UnixMilli(1_700_000_000_000))
	b := NewAt(time.UnixMilli(1_700_000_000_100))
	if len(a) != 26 || len(b) != 26 {
		t.Fatalf("length %d %d", len(a), len(b))
	}
	if a >= b {
		t.Fatalf("time order lost: %s >= %s", a, b)
	}
	for _, c := range a {
		if !stringsContains(crockford, c) {
			t.Fatalf("bad char %q", c)
		}
	}
}

func stringsContains(s string, c rune) bool {
	for _, x := range s {
		if x == c {
			return true
		}
	}
	return false
}
