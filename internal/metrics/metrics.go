// Package metrics renders a small Prometheus text exposition.
package metrics

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Metrics collects counters. Label sets are encoded into the map key.
type Metrics struct {
	mu       sync.Mutex
	counters map[string]uint64
}

// New returns an empty registry.
func New() *Metrics {
	return &Metrics{counters: map[string]uint64{}}
}

// Inc adds one to a counter. labels may be nil.
func (m *Metrics) Inc(name string, labels map[string]string) {
	if m == nil {
		return
	}
	m.Add(name, 1, labels)
}

// Add adds n to a counter.
func (m *Metrics) Add(name string, n uint64, labels map[string]string) {
	if m == nil || n == 0 {
		return
	}
	key := seriesKey(name, labels)
	m.mu.Lock()
	m.counters[key] += n
	m.mu.Unlock()
}

// Snapshot copies the counters.
func (m *Metrics) Snapshot() map[string]uint64 {
	out := map[string]uint64{}
	if m == nil {
		return out
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range m.counters {
		out[k] = v
	}
	return out
}

// Render writes counters plus the supplied gauges in Prometheus text format.
func (m *Metrics) Render(gauges map[string]float64) string {
	var b strings.Builder
	writeHelp(&b, "mcon_up", "gauge", "1 when the process is serving.")
	fmt.Fprintf(&b, "mcon_up 1\n")

	counters := m.Snapshot()
	names := map[string]struct{}{}
	keys := make([]string, 0, len(counters))
	for k := range counters {
		keys = append(keys, k)
		names[seriesName(k)] = struct{}{}
	}
	sort.Strings(keys)
	known := []string{
		"mcon_messages_accepted_total",
		"mcon_provider_results_total",
		"mcon_spam_decisions_total",
		"mcon_webhook_results_total",
	}
	help := map[string]string{
		"mcon_messages_accepted_total": "Messages accepted by the public API.",
		"mcon_provider_results_total":  "Provider send attempts by result.",
		"mcon_spam_decisions_total":    "Spam pipeline decisions.",
		"mcon_webhook_results_total":   "Webhook delivery attempts by result.",
	}
	for _, name := range known {
		if _, ok := names[name]; !ok && !hasPrefix(keys, name) {
			writeHelp(&b, name, "counter", help[name])
			continue
		}
		writeHelp(&b, name, "counter", help[name])
		for _, k := range keys {
			if seriesName(k) == name {
				fmt.Fprintf(&b, "%s %d\n", k, counters[k])
			}
		}
	}
	gkeys := make([]string, 0, len(gauges))
	for k := range gauges {
		gkeys = append(gkeys, k)
	}
	sort.Strings(gkeys)
	if len(gkeys) > 0 {
		writeHelp(&b, "mcon_messages_current", "gauge", "Messages currently stored, by state.")
		writeHelp(&b, "mcon_queue_depth", "gauge", "Messages waiting for or in delivery.")
		for _, k := range gkeys {
			fmt.Fprintf(&b, "%s %g\n", k, gauges[k])
		}
	}
	return b.String()
}

func hasPrefix(keys []string, name string) bool {
	for _, k := range keys {
		if seriesName(k) == name {
			return true
		}
	}
	return false
}

func writeHelp(b *strings.Builder, name, typ, help string) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
}

func seriesKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(name)
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%s=%q", k, labels[k])
	}
	b.WriteByte('}')
	return b.String()
}

func seriesName(key string) string {
	if i := strings.IndexByte(key, '{'); i >= 0 {
		return key[:i]
	}
	return key
}
