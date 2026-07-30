package streamnotifier

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"martie/internal/apps/streamnotifier/probe"
	streamnotifierstate "martie/internal/apps/streamnotifier/state"
	"martie/internal/telegram"
)

const (
	endMissThreshold = 2

	Source             = "streamnotifier"
	NotificationSent   = "sent"
	NotificationFailed = "failed"
)

type Config struct {
	Channels     []probe.Channel
	PollInterval time.Duration
}

type Poller struct {
	Channels []probe.Channel
	Format   Formatter
	ChatID   int64
	Store    Store
	Client   Client
	Telegram Sender
	Metrics  Metrics
	Logger   *slog.Logger
}

type Client interface {
	IsLive(context.Context, probe.Channel) (bool, error)
}

type Store interface {
	GetStreamState(context.Context, string) (streamnotifierstate.Stream, bool, error)
	UpsertStreamState(context.Context, streamnotifierstate.Stream) error
}

type Sender interface {
	Send(context.Context, telegram.SendRequest) error
}

type Metrics interface {
	ObserveNotification(source, result string)
}

func (s Poller) Poll(ctx context.Context) error {
	var errs []error
	for _, channel := range s.Channels {
		if err := s.pollChannel(ctx, channel); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s Poller) pollChannel(ctx context.Context, channel probe.Channel) error {
	live, err := s.Client.IsLive(ctx, channel)
	if err != nil {
		return fmt.Errorf("check stream %s: %w", channel.Key, err)
	}

	stream, _, err := s.Store.GetStreamState(ctx, channel.Key)
	if err != nil {
		return fmt.Errorf("load stream %s: %w", channel.Key, err)
	}

	if live {
		return s.handleStartedStream(ctx, channel, stream)
	}
	return s.handleStoppedStream(ctx, channel, stream)
}

func (s Poller) handleStartedStream(ctx context.Context, channel probe.Channel, stream streamnotifierstate.Stream) error {
	wasActive := stream.Active
	previousMisses := stream.Consecutive404s
	stream.Key = channel.Key
	stream.Active = true
	stream.Consecutive404s = 0

	if wasActive && stream.LiveNotified {
		if previousMisses == 0 {
			return nil
		}
		return s.storeStreamState(ctx, channel.Key, stream)
	}

	stream.LiveNotified = false
	if err := s.storeStreamState(ctx, channel.Key, stream); err != nil {
		return err
	}

	message := s.Format.LiveNotification(LiveNotice{PageURL: channel.PageURL})
	if err := s.Telegram.Send(ctx, telegram.SendRequest{ChatID: s.ChatID, Message: message}); err != nil {
		s.Metrics.ObserveNotification(Source, NotificationFailed)
		return fmt.Errorf("send stream telegram message for %s: %w", channel.Key, err)
	}
	s.Logger.Info("stream live notification sent", "stream", channel.Key)
	s.Metrics.ObserveNotification(Source, NotificationSent)

	stream.LiveNotified = true
	if err := s.Store.UpsertStreamState(ctx, stream); err != nil {
		s.Logger.Warn("notification sent but stream could not be marked notified", "stream", channel.Key, "error", err)
	}

	return nil
}

func (s Poller) handleStoppedStream(ctx context.Context, channel probe.Channel, stream streamnotifierstate.Stream) error {
	if !stream.Active {
		return nil
	}

	stream.Consecutive404s++
	if stream.Consecutive404s < endMissThreshold {
		return s.storeStreamState(ctx, channel.Key, stream)
	}

	stream.Active = false
	stream.LiveNotified = false
	stream.Consecutive404s = 0
	s.Logger.Info("stream marked offline", "stream", channel.Key, "misses", endMissThreshold)
	return s.storeStreamState(ctx, channel.Key, stream)
}

func (s Poller) storeStreamState(ctx context.Context, channelKey string, stream streamnotifierstate.Stream) error {
	if err := s.Store.UpsertStreamState(ctx, stream); err != nil {
		return fmt.Errorf("store stream %s: %w", channelKey, err)
	}
	return nil
}
