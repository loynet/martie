package state

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestFormatTimeIsFixedWidth(t *testing.T) {
	got := formatTime(time.Date(2026, time.April, 14, 12, 34, 56, 120000000, time.UTC))
	want := "2026-04-14T12:34:56.120000000Z"

	if got != want {
		t.Fatalf("formatTime() = %q, want %q", got, want)
	}
}

func TestParseTimeAcceptsOldAndNewFormats(t *testing.T) {
	tests := []string{
		"2026-04-14T12:34:56.120000000Z",
		time.Date(2026, time.April, 14, 12, 34, 56, 120000000, time.UTC).Format(time.RFC3339Nano),
	}

	want := time.Date(2026, time.April, 14, 12, 34, 56, 120000000, time.UTC)

	for _, tc := range tests {
		got, err := parseTime(tc)
		if err != nil {
			t.Fatalf("parseTime(%q) error = %v", tc, err)
		}
		if !got.Equal(want) {
			t.Fatalf("parseTime(%q) = %v, want %v", tc, got, want)
		}
	}
}

func TestEnsureGatewayBootstrapAtPreservesNanoseconds(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	now := time.Date(2026, time.July, 20, 12, 0, 0, 123456789, time.UTC)
	got, err := store.EnsureGatewayBootstrapAt(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(now) {
		t.Fatalf("EnsureGatewayBootstrapAt() = %v, want %v", got, now)
	}

	got, err = store.EnsureGatewayBootstrapAt(context.Background(), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(now) {
		t.Fatalf("restored bootstrap = %v, want %v", got, now)
	}
}

func TestEnsureGatewayBootstrapAtReadsLegacySecondCursor(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	want := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	if err := store.SetCursor(context.Background(), "gateway_bootstrap_at", want.Unix()); err != nil {
		t.Fatal(err)
	}

	got, err := store.EnsureGatewayBootstrapAt(context.Background(), want.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(want) {
		t.Fatalf("legacy bootstrap = %v, want %v", got, want)
	}
}
