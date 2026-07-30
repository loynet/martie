package streamnotifier

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"martie/internal/apps/streamnotifier/probe"
	streamnotifierstate "martie/internal/apps/streamnotifier/state"
	"martie/internal/localization"
	"martie/internal/telegram"
)

func TestStreamPollContinuesAfterChannelFailure(t *testing.T) {
	client := &fakeStreamClient{fail: "first"}
	watcher := Poller{
		Channels: []probe.Channel{{Key: "first"}, {Key: "second"}},
		Client:   client,
		Store:    &fakeStreamStore{},
		Telegram: &fakeMessageSender{},
	}

	err := watcher.Poll(context.Background())
	if err == nil || !errors.Is(err, errStreamCheck) {
		t.Fatalf("poll() error = %v, want stream check error", err)
	}
	if len(client.checked) != 2 || client.checked[0] != "first" || client.checked[1] != "second" {
		t.Fatalf("checked channels = %v, want [first second]", client.checked)
	}
}

func TestStartedStreamIsMarkedNotifiedAfterSend(t *testing.T) {
	store := &fakeStreamStore{}
	sender := &fakeMessageSender{}
	watcher := Poller{
		ChatID:   42,
		Format:   NewFormatter(localization.New(localization.English)),
		Store:    store,
		Telegram: sender,
		Metrics:  &fakeMetrics{},
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	channel := probe.Channel{Key: "live", PageURL: "https://example.com/live"}

	if err := watcher.handleStartedStream(context.Background(), channel, streamnotifierstate.Stream{}); err != nil {
		t.Fatalf("handleStartedStream() error = %v", err)
	}
	if len(sender.requests) != 1 {
		t.Fatalf("sent messages = %d, want 1", len(sender.requests))
	}
	if !store.state.Active || !store.state.LiveNotified || store.state.Key != "live" {
		t.Fatalf("stored state = %+v, want active and notified", store.state)
	}
}

func TestStartedStreamCountsFailedNotification(t *testing.T) {
	metrics := &fakeMetrics{}
	watcher := Poller{
		ChatID:   42,
		Format:   NewFormatter(localization.New(localization.English)),
		Store:    &fakeStreamStore{},
		Telegram: &fakeMessageSender{err: errors.New("send failed")},
		Metrics:  metrics,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	err := watcher.handleStartedStream(context.Background(), probe.Channel{Key: "live", PageURL: "https://example.com/live"}, streamnotifierstate.Stream{})
	if err == nil {
		t.Fatal("handleStartedStream() error = nil")
	}
	if metrics.results[NotificationFailed] != 1 {
		t.Fatalf("failed notification count = %d, want 1", metrics.results[NotificationFailed])
	}
}

func TestStoppedStreamRequiresConsecutiveMisses(t *testing.T) {
	store := &fakeStreamStore{}
	watcher := Poller{Store: store, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	channel := probe.Channel{Key: "live"}
	stream := streamnotifierstate.Stream{Key: "live", Active: true, LiveNotified: true}

	if err := watcher.handleStoppedStream(context.Background(), channel, stream); err != nil {
		t.Fatalf("first handleStoppedStream() error = %v", err)
	}
	if !store.state.Active || store.state.Consecutive404s != 1 {
		t.Fatalf("state after first miss = %+v, want active with one miss", store.state)
	}
	if err := watcher.handleStoppedStream(context.Background(), channel, store.state); err != nil {
		t.Fatalf("second handleStoppedStream() error = %v", err)
	}
	if store.state.Active || store.state.LiveNotified || store.state.Consecutive404s != 0 {
		t.Fatalf("state after second miss = %+v, want reset offline state", store.state)
	}
}

var errStreamCheck = errors.New("stream check failed")

type fakeStreamClient struct {
	fail    string
	checked []string
}

func (f *fakeStreamClient) IsLive(_ context.Context, channel probe.Channel) (bool, error) {
	f.checked = append(f.checked, channel.Key)
	if channel.Key == f.fail {
		return false, errStreamCheck
	}
	return false, nil
}

type fakeStreamStore struct {
	state streamnotifierstate.Stream
}

func (*fakeStreamStore) GetStreamState(context.Context, string) (streamnotifierstate.Stream, bool, error) {
	return streamnotifierstate.Stream{}, false, nil
}

func (f *fakeStreamStore) UpsertStreamState(_ context.Context, stream streamnotifierstate.Stream) error {
	f.state = stream
	return nil
}

type fakeMessageSender struct {
	requests []telegram.SendRequest
	err      error
}

type fakeMetrics struct {
	results map[string]int
}

func (f *fakeMetrics) ObserveNotification(_ string, result string) {
	if f.results == nil {
		f.results = make(map[string]int)
	}
	f.results[result]++
}

func (f *fakeMessageSender) Send(_ context.Context, request telegram.SendRequest) error {
	f.requests = append(f.requests, request)
	return f.err
}
