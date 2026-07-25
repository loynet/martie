package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"martie/internal/miau"
	"martie/internal/state"
	"martie/internal/telegram"
)

type streamPoller struct {
	channels         []miau.Channel
	format           telegram.Formatter
	endMissThreshold int
	chatID           int64
	store            streamStore
	client           streamClient
	telegram         messageSender
	metrics          *metrics
	logger           *slog.Logger
}

type streamClient interface {
	IsLive(context.Context, miau.Channel) (bool, error)
}

type streamStore interface {
	GetStreamState(context.Context, string) (state.StreamState, bool, error)
	UpsertStreamState(context.Context, state.StreamState) error
}

func (s streamPoller) poll(ctx context.Context) error {
	var errs []error
	for _, channel := range s.channels {
		if err := s.pollChannel(ctx, channel); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s streamPoller) pollChannel(ctx context.Context, channel miau.Channel) error {
	live, err := s.client.IsLive(ctx, channel)
	if err != nil {
		return fmt.Errorf("check miau stream %s: %w", channel.Key, err)
	}

	stream, _, err := s.store.GetStreamState(ctx, miauStateKey(channel.Key))
	if err != nil {
		return fmt.Errorf("load miau stream %s: %w", channel.Key, err)
	}

	if live {
		return s.handleStartedStream(ctx, channel, stream)
	}
	return s.handleStoppedStream(ctx, channel, stream)
}

func (s streamPoller) handleStartedStream(ctx context.Context, channel miau.Channel, stream state.StreamState) error {
	wasActive := stream.Active
	previousMisses := stream.Consecutive404s
	stream.Key = miauStateKey(channel.Key)
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

	message := s.format.MiauStreamNotification(telegram.MiauStreamNotice{PageURL: channel.PageURL})
	if err := s.telegram.Send(ctx, telegram.SendRequest{ChatID: s.chatID, Message: message}); err != nil {
		return fmt.Errorf("send miau telegram message for %s: %w", channel.Key, err)
	}
	s.logger.Info("stream live notification sent", "stream", channel.Key)
	s.metrics.addNotifications(string(componentStreams), 1)

	stream.LiveNotified = true
	if err := s.store.UpsertStreamState(ctx, stream); err != nil {
		s.logger.Warn("notification sent but stream could not be marked notified", "stream", channel.Key, "error", err)
	}

	return nil
}

func (s streamPoller) handleStoppedStream(ctx context.Context, channel miau.Channel, stream state.StreamState) error {
	if !stream.Active {
		return nil
	}

	stream.Consecutive404s++
	if stream.Consecutive404s < s.endMissThreshold {
		return s.storeStreamState(ctx, channel.Key, stream)
	}

	stream.Active = false
	stream.LiveNotified = false
	stream.Consecutive404s = 0
	s.logger.Info("stream marked offline", "stream", channel.Key, "misses", s.endMissThreshold)
	return s.storeStreamState(ctx, channel.Key, stream)
}

func (s streamPoller) storeStreamState(ctx context.Context, channelKey string, stream state.StreamState) error {
	if err := s.store.UpsertStreamState(ctx, stream); err != nil {
		return fmt.Errorf("store miau stream %s: %w", channelKey, err)
	}
	return nil
}

func miauStateKey(channelKey string) string {
	return "miau:" + channelKey
}
