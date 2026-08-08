package main

import "testing"

func TestParseCommand(t *testing.T) {
	for _, command := range []string{"run", "check-config", "check-health"} {
		got, err := parseCommand([]string{command})
		if err != nil || got != command {
			t.Fatalf("parseCommand(%q) = %q, %v", command, got, err)
		}
	}
	for _, args := range [][]string{nil, {"run", "extra"}, {"invalid"}} {
		if _, err := parseCommand(args); err == nil {
			t.Fatalf("parseCommand(%q) succeeded", args)
		}
	}
}
