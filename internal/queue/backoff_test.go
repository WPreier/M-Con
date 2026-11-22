package queue

import (
	"testing"
	"time"
)

func TestBackoff(t *testing.T) {
	if got := Backoff(1, 5*time.Second, time.Hour, 2); got != 5*time.Second {
		t.Fatal(got)
	}
	if got := Backoff(2, 5*time.Second, time.Hour, 2); got != 10*time.Second {
		t.Fatal(got)
	}
	if got := Backoff(3, 5*time.Second, time.Hour, 2); got != 20*time.Second {
		t.Fatal(got)
	}
	if got := Backoff(20, 5*time.Second, 30*time.Second, 2); got != 30*time.Second {
		t.Fatal(got)
	}
}
