package main

import "testing"

func TestParseCommand(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{want: "run"},
		{args: []string{"run"}, want: "run"},
		{args: []string{"check-config"}, want: "check-config"},
		{args: []string{"check-health"}, want: "check-health"},
	}

	for _, test := range tests {
		got, err := parseCommand(test.args)
		if err != nil {
			t.Fatalf("parseCommand(%v) error = %v", test.args, err)
		}
		if got != test.want {
			t.Fatalf("parseCommand(%v) = %q, want %q", test.args, got, test.want)
		}
	}
}

func TestParseCommandRejectsUnsupportedCommands(t *testing.T) {
	if _, err := parseCommand([]string{"check-config", "extra"}); err == nil {
		t.Fatal("extra argument was accepted")
	}
	if _, err := parseCommand([]string{"nope"}); err == nil {
		t.Fatal("unsupported command was accepted")
	}
}
