package message

import (
	"strings"
	"testing"
	"time"
)

func TestRejectsHeaderInjection(t *testing.T) {
	err := Validate(Submission{
		From: "noreply@example.com", To: []string{"a@b.c"}, Subject: "Hi\nBcc: evil@x", Text: "x",
	}, Limits{})
	if err == nil || !strings.Contains(err.Error(), "injection") {
		t.Fatalf("got %v", err)
	}
	err = Validate(Submission{
		From: "noreply@example.com", To: []string{"a@b.c"}, Subject: "Hi", Text: "x",
		Headers: map[string]string{"X-Test": "a\r\nbcc: z@z"},
	}, Limits{})
	if err == nil {
		t.Fatal("expected header rejection")
	}
}

func TestRFC822HardeningAndHash(t *testing.T) {
	sub := Submission{
		From: "noreply@example.com", To: []string{"a@b.c"}, Cc: []string{"c@d.e"}, Bcc: []string{"hidden@e.f"},
		ReplyTo: "user@b.c", Subject: "Hello", Text: "plain", HTML: "<p>html</p>",
		Headers:     map[string]string{"X-Form": "contact"},
		Attachments: []Attachment{{Filename: "note.txt", ContentType: "text/plain", Data: []byte("file")}},
	}
	if err := Validate(sub, Limits{MaxRecipients: 10, MaxMessageBytes: 100000, MaxAttachments: 5, AllowedFrom: []string{"noreply@example.com"}}); err != nil {
		t.Fatal(err)
	}
	h1 := BodyHash(sub)
	swapped := sub
	swapped.To = []string{"a@b.c"}
	swapped.Cc = []string{"c@d.e"}
	if BodyHash(swapped) != h1 {
		t.Fatal("hash unstable")
	}
	raw, err := Build(Outbound{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", From: sub.From, To: sub.To, Cc: sub.Cc, Bcc: sub.Bcc,
		ReplyTo: sub.ReplyTo, Subject: sub.Subject, Text: sub.Text, HTML: sub.HTML,
		Headers: sub.Headers, Attachments: sub.Attachments,
		MessageID: MessageID("01ARZ3NDEKTSV4RRFFQ69G5FAV", sub.From),
		Date:      time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"X-MCon-Id: 01ARZ3NDEKTSV4RRFFQ69G5FAV", "From:", "Message-ID:", "X-Form: contact", "note.txt"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s in\n%s", want, text)
		}
	}
	if strings.Contains(text, "hidden@e.f") {
		t.Fatal("bcc leaked into header")
	}
	if strings.Contains(text, "\nBcc:") || strings.Contains(text, "\r\nBcc:") {
		t.Fatal("unexpected bcc header")
	}
	if ContentHash(raw) == "" || ContentHash(raw) == h1 {
		t.Fatal("content hash should differ from body hash")
	}
	again, err := Build(Outbound{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", From: sub.From, To: sub.To, Cc: sub.Cc, Bcc: sub.Bcc,
		ReplyTo: sub.ReplyTo, Subject: sub.Subject, Text: sub.Text, HTML: sub.HTML,
		Headers: sub.Headers, Attachments: sub.Attachments,
		MessageID: MessageID("01ARZ3NDEKTSV4RRFFQ69G5FAV", sub.From),
		Date:      time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Boundaries are random, so the bytes differ. The stable headers must match.
	if !strings.Contains(string(again), "Subject: Hello") {
		t.Fatal(string(again))
	}
}

func TestBodyHashIgnoresAttachmentOrder(t *testing.T) {
	a := Submission{From: "a@b.c", To: []string{"c@d.e"}, Subject: "s", Text: "t",
		Attachments: []Attachment{{Filename: "b.txt", Data: []byte("b")}, {Filename: "a.txt", Data: []byte("a")}}}
	b := Submission{From: "a@b.c", To: []string{"c@d.e"}, Subject: "s", Text: "t",
		Attachments: []Attachment{{Filename: "a.txt", Data: []byte("a")}, {Filename: "b.txt", Data: []byte("b")}}}
	if BodyHash(a) != BodyHash(b) {
		t.Fatal("attachment order changed the body hash")
	}
}
