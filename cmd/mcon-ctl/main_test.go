package main

import "testing"

func TestSplitCommand(t *testing.T) {
	tests := []struct {
		args     []string
		wantCmd  string
		wantFlags []string
		ok       bool
	}{
		{[]string{"health"}, "health", nil, true},
		{[]string{"-config", "x.yaml", "health"}, "health", []string{"-config", "x.yaml"}, true},
		{[]string{"health", "-config", "x.yaml"}, "health", []string{"-config", "x.yaml"}, true},
		{[]string{"send", "--file", "a.json", "-config", "c.yaml"}, "send", []string{"--file", "a.json", "-config", "c.yaml"}, true},
		{[]string{"-config", "c.yaml", "send", "--file", "a.json"}, "send", []string{"-config", "c.yaml", "--file", "a.json"}, true},
		{[]string{"-config", "x.yaml"}, "", nil, false},
		{[]string{"nope"}, "", nil, false},
	}
	for _, tc := range tests {
		cmd, flags, ok := splitCommand(tc.args)
		if ok != tc.ok || cmd != tc.wantCmd {
			t.Fatalf("args %v: cmd=%q ok=%v want %q/%v", tc.args, cmd, ok, tc.wantCmd, tc.ok)
		}
		if !ok {
			continue
		}
		if len(flags) != len(tc.wantFlags) {
			t.Fatalf("args %v: flags %v want %v", tc.args, flags, tc.wantFlags)
		}
		for i := range flags {
			if flags[i] != tc.wantFlags[i] {
				t.Fatalf("args %v: flags %v want %v", tc.args, flags, tc.wantFlags)
			}
		}
	}
}
