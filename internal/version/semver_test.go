package version

import "testing"

func TestBump(t *testing.T) {
	cases := []struct {
		in, kind, want string
	}{
		{"0.1.0", "patch", "0.1.1"},
		{"0.1.1", "minor", "0.2.0"},
		{"0.2.3", "major", "1.0.0"},
		{"1.4.9", "patch", "1.4.10"},
	}
	for _, tc := range cases {
		got, err := Bump(tc.in, tc.kind)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Fatalf("Bump(%s,%s)=%s want %s", tc.in, tc.kind, got, tc.want)
		}
	}
}

func TestRequiredBump(t *testing.T) {
	cases := map[string]string{
		"feat: add queue\n":                         "minor",
		"feat(api): accept mail\n":                  "minor",
		"fix: lease leak\n":                         "patch",
		"fix(smtp): classify 450\n":                 "patch",
		"feat!: drop legacy header\n":               "major",
		"fix!: reject empty from\n":                 "major",
		"feat: add\n\nBREAKING CHANGE: id format\n": "major",
		"docs: readme\n":                            "",
		"chore: tidy\n":                             "",
		"test: cover backoff\n":                     "",
	}
	for msg, want := range cases {
		if got := RequiredBump(msg); got != want {
			t.Fatalf("RequiredBump(%q)=%q want %q", msg, got, want)
		}
	}
}

func TestParseRejects(t *testing.T) {
	for _, v := range []string{"1.0", "v1.0.0", "01.0.0", "1.0.a"} {
		if _, _, _, err := Parse(v); err == nil {
			t.Fatalf("Parse(%s) succeeded", v)
		}
	}
}
