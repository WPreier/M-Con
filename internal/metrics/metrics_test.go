package metrics

import (
	"strings"
	"testing"
)

func TestRenderCounterAndGauge(t *testing.T) {
	m := New()
	m.Inc("mcon_messages_accepted_total", nil)
	m.Inc("mcon_provider_results_total", map[string]string{"provider": "smtp-local", "result": "sent"})
	text := m.Render(map[string]float64{
		`mcon_messages_current{state="queued"}`: 2,
		"mcon_queue_depth":                      2,
	})
	for _, want := range []string{
		"mcon_up 1",
		"mcon_messages_accepted_total 1",
		`mcon_provider_results_total{provider="smtp-local",result="sent"} 1`,
		`mcon_messages_current{state="queued"} 2`,
		"# TYPE mcon_queue_depth gauge",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in\n%s", want, text)
		}
	}
}
