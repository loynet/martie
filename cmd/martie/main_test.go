package main

import (
	"testing"

	"martie/internal/app"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		args    []string
		command string
		appName app.AppName
	}{
		{args: []string{"chatter"}, command: "run", appName: app.AppChatter},
		{args: []string{"channer"}, command: "run", appName: app.AppChanner},
		{args: []string{"threadnotifier"}, command: "run", appName: app.AppThreadNotifier},
		{args: []string{"streamnotifier"}, command: "run", appName: app.AppStreamNotifier},
		{args: []string{"check-config", "chatter"}, command: "check-config", appName: app.AppChatter},
		{args: []string{"check-config", "channer"}, command: "check-config", appName: app.AppChanner},
		{args: []string{"check-config", "threadnotifier"}, command: "check-config", appName: app.AppThreadNotifier},
		{args: []string{"check-health"}, command: "check-health"},
	}

	for _, test := range tests {
		command, appName, err := parseCommand(test.args)
		if err != nil {
			t.Fatalf("parseCommand(%v) error = %v", test.args, err)
		}
		if command != test.command || appName != test.appName {
			t.Fatalf("parseCommand(%v) = %q %q, want %q %q", test.args, command, appName, test.command, test.appName)
		}
	}
}

func TestParseCommandRejectsUnsupportedCommands(t *testing.T) {
	if _, _, err := parseCommand(nil); err == nil {
		t.Fatal("missing app command was accepted")
	}
	if _, _, err := parseCommand([]string{"check-config"}); err == nil {
		t.Fatal("check-config without app was accepted")
	}
	if _, _, err := parseCommand([]string{"run"}); err == nil {
		t.Fatal("legacy run command was accepted")
	}
	if _, _, err := parseCommand([]string{"assistant"}); err == nil {
		t.Fatal("aggregate assistant command was accepted")
	}
	if _, _, err := parseCommand([]string{"nope"}); err == nil {
		t.Fatal("unsupported command was accepted")
	}
}
