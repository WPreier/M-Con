// Package spam classifies messages with Ollama and an OpenRouter failover.
package spam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"mcon/internal/config"
)

// Input is the text the classifier is allowed to see. Attachments are omitted.
type Input struct {
	From    string
	To      []string
	Subject string
	Text    string
	HTML    string
	Hint    string
}

// Verdict is the model output.
type Verdict struct {
	Spam   bool
	Score  float64
	Reason string
	Engine string
}

// Decision is what the queue should do after the timeout policy.
type Decision struct {
	Action string
	Spam   bool
	Score  float64
	Reason string
	Engine string
}

// Pipeline calls the configured engines.
type Pipeline struct {
	Cfg    func() config.Spam
	APIKey func() string
	HTTP   *http.Client
}

// Prompt builds the classifier prompt. It never includes attachment bytes.
func Prompt(in Input) string {
	html := stripTags(in.HTML)
	if len(html) > 4000 {
		html = html[:4000]
	}
	text := in.Text
	if len(text) > 4000 {
		text = text[:4000]
	}
	return "You classify contact-form mail. Reply with ONLY a JSON object {\"spam\":bool,\"score\":number between 0 and 1,\"reason\":string}. No markdown.\n" +
		"From: " + in.From + "\nTo: " + strings.Join(in.To, ", ") + "\nSubject: " + in.Subject +
		"\nText: " + text + "\nHTML: " + html + "\nHint: " + in.Hint + "\n"
}

// Decide applies the configured timeout and error policy.
func Decide(cfg config.Spam, v *Verdict, callErr error) Decision {
	if !cfg.Enabled {
		return Decision{Action: "send", Engine: "disabled", Reason: "spam check disabled"}
	}
	if callErr != nil {
		if isTimeout(callErr) {
			return Decision{Action: orAction(cfg.OnTimeout), Engine: "timeout", Reason: "timeout"}
		}
		return Decision{Action: orAction(cfg.OnError), Engine: "error", Reason: callErr.Error()}
	}
	if v == nil {
		return Decision{Action: orAction(cfg.OnError), Engine: "error", Reason: "empty verdict"}
	}
	spam := v.Spam || v.Score >= cfg.Threshold
	action := "send"
	if spam {
		action = "block"
	}
	return Decision{Action: action, Spam: spam, Score: v.Score, Reason: v.Reason, Engine: v.Engine}
}

func orAction(v string) string {
	if v == "block" {
		return "block"
	}
	return "send"
}

// Evaluate runs the configured engines inside the overall timeout.
func (p *Pipeline) Evaluate(ctx context.Context, in Input) (Decision, error) {
	cfg := config.Spam{Enabled: false, OnTimeout: "send", OnError: "send"}
	if p != nil && p.Cfg != nil {
		cfg = p.Cfg()
	}
	if !cfg.Enabled {
		return Decide(cfg, nil, nil), nil
	}
	if ctx.Err() != nil {
		return Decision{}, ctx.Err()
	}
	parent, cancel := context.WithTimeout(ctx, cfg.Timeout.Std())
	defer cancel()
	var v *Verdict
	var err error
	switch cfg.Mode {
	case "openrouter":
		v, err = p.callOpenRouter(parent, cfg, in)
	case "ollama":
		v, err = p.callOllama(parent, cfg, in)
	default:
		octx, ocancel := context.WithTimeout(parent, cfg.Ollama.Timeout.Std())
		v, err = p.callOllama(octx, cfg, in)
		ocancel()
		if err != nil {
			v, err = p.callOpenRouter(parent, cfg, in)
		}
	}
	if ctx.Err() != nil {
		return Decision{}, ctx.Err()
	}
	return Decide(cfg, v, err), nil
}

func (p *Pipeline) callOllama(ctx context.Context, cfg config.Spam, in Input) (*Verdict, error) {
	body, _ := json.Marshal(map[string]any{
		"model": cfg.Ollama.Model, "stream": false, "format": "json",
		"messages": []map[string]string{{"role": "user", "content": Prompt(in)}},
	})
	buf, err := p.post(ctx, strings.TrimRight(cfg.Ollama.BaseURL, "/")+"/api/chat", "", body)
	if err != nil {
		return nil, err
	}
	var env struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(buf, &env); err != nil {
		return nil, err
	}
	v, err := parseVerdict(env.Message.Content)
	if err != nil {
		return nil, err
	}
	v.Engine = "ollama"
	return v, nil
}

func (p *Pipeline) callOpenRouter(ctx context.Context, cfg config.Spam, in Input) (*Verdict, error) {
	body, _ := json.Marshal(map[string]any{
		"model":    cfg.OpenRouter.Model,
		"messages": []map[string]string{{"role": "user", "content": Prompt(in)}},
	})
	key := ""
	if p.APIKey != nil {
		key = p.APIKey()
	}
	buf, err := p.post(ctx, strings.TrimRight(cfg.OpenRouter.BaseURL, "/")+"/chat/completions", key, body)
	if err != nil {
		return nil, err
	}
	var env struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(buf, &env); err != nil {
		return nil, err
	}
	if len(env.Choices) == 0 {
		return nil, errors.New("openrouter empty choices")
	}
	v, err := parseVerdict(env.Choices[0].Message.Content)
	if err != nil {
		return nil, err
	}
	v.Engine = "openrouter"
	return v, nil
}

func (p *Pipeline) post(ctx context.Context, url, bearer string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	client := p.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	buf, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		return nil, errors.New(strings.TrimSpace(string(buf)))
	}
	return buf, nil
}

func parseVerdict(content string) (*Verdict, error) {
	content = strings.TrimSpace(content)
	if i := strings.Index(content, "{"); i >= 0 {
		if j := strings.LastIndex(content, "}"); j > i {
			content = content[i : j+1]
		}
	}
	var raw struct {
		Spam   bool    `json:"spam"`
		Score  float64 `json:"score"`
		Reason string  `json:"reason"`
	}
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return nil, err
	}
	if raw.Score < 0 {
		raw.Score = 0
	}
	if raw.Score > 1 {
		raw.Score = 1
	}
	return &Verdict{Spam: raw.Spam, Score: raw.Score, Reason: raw.Reason}, nil
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func stripTags(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}
