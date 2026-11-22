package spam

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mcon/internal/config"
)

func TestDecisionMatrix(t *testing.T) {
	base := config.Spam{Enabled: true, Threshold: 0.8, OnTimeout: "send", OnError: "block"}
	cases := []struct {
		name   string
		cfg    config.Spam
		v      *Verdict
		err    error
		action string
	}{
		{"disabled", config.Spam{Enabled: false}, nil, nil, "send"},
		{"ham", base, &Verdict{Score: 0.1, Engine: "ollama"}, nil, "send"},
		{"score", base, &Verdict{Score: 0.9}, nil, "block"},
		{"flag", base, &Verdict{Spam: true, Score: 0.1}, nil, "block"},
		{"timeout-send", base, nil, context.DeadlineExceeded, "send"},
		{"timeout-block", config.Spam{Enabled: true, Threshold: 0.8, OnTimeout: "block", OnError: "send"}, nil, context.DeadlineExceeded, "block"},
		{"error-block", base, nil, context.Canceled, "block"},
		{"error-send", config.Spam{Enabled: true, OnTimeout: "block", OnError: "send", Threshold: 0.8}, nil, errString("boom"), "send"},
	}
	for _, tc := range cases {
		got := Decide(tc.cfg, tc.v, tc.err)
		if got.Action != tc.action {
			t.Fatalf("%s action %s", tc.name, got.Action)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestFailoverAndTimeout(t *testing.T) {
	var ollamaHits, openHits int
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ollamaHits++
		time.Sleep(80 * time.Millisecond)
		_, _ = w.Write([]byte(`{"message":{"content":"{\"spam\":true,\"score\":0.99,\"reason\":\"slow\"}"}}`))
	}))
	defer ollama.Close()
	open := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		openHits++
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Errorf("auth")
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"spam\":false,\"score\":0.2,\"reason\":\"ok\"}"}}]}`))
	}))
	defer open.Close()
	p := &Pipeline{
		HTTP:   open.Client(),
		APIKey: func() string { return "key" },
		Cfg: func() config.Spam {
			return config.Spam{
				Enabled: true, Mode: "failover", Timeout: config.Duration(time.Second),
				OnTimeout: "block", OnError: "block", Threshold: 0.8,
				Ollama:     config.Ollama{BaseURL: ollama.URL, Model: "m", Timeout: config.Duration(30 * time.Millisecond)},
				OpenRouter: config.OpenRouter{BaseURL: open.URL, Model: "m", Timeout: config.Duration(time.Second)},
			}
		},
	}
	// The ollama client is a different server. Use a client that can reach both.
	p.HTTP = http.DefaultClient
	dec, err := p.Evaluate(context.Background(), Input{From: "a@b.c", To: []string{"c@d.e"}, Subject: "hi", Text: "hello", HTML: "<b>x</b>"})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Action != "send" || dec.Engine != "openrouter" || ollamaHits == 0 || openHits == 0 {
		t.Fatalf("%+v hits %d %d", dec, ollamaHits, openHits)
	}
}

func TestPromptOmitsAttachments(t *testing.T) {
	got := Prompt(Input{Subject: "Invoice", Text: "please pay", HTML: "<p>now</p>"})
	if !strings.Contains(got, "Invoice") || strings.Contains(got, "SECRET-BYTES") {
		t.Fatal(got)
	}
	if strings.Contains(got, "<p>") {
		t.Fatal("tags leaked")
	}
}

func TestOllamaTimeoutPolicy(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer slow.Close()
	p := &Pipeline{HTTP: slow.Client(), Cfg: func() config.Spam {
		return config.Spam{
			Enabled: true, Mode: "ollama", Timeout: config.Duration(40 * time.Millisecond),
			OnTimeout: "block", OnError: "send", Threshold: 0.8,
			Ollama: config.Ollama{BaseURL: slow.URL, Model: "m", Timeout: config.Duration(40 * time.Millisecond)},
		}
	}}
	dec, err := p.Evaluate(context.Background(), Input{Subject: "s", Text: "t"})
	if err != nil || dec.Action != "block" || dec.Engine != "timeout" {
		t.Fatalf("%+v %v", dec, err)
	}
}
