package threadnotifier

import (
	"fmt"
	"strings"
	"time"

	"martie/internal/localization"
	"martie/internal/telegram"
)

type Formatter struct {
	text localization.Localizer
}

type ThreadNotice struct {
	Board      string
	PostID     int64
	Date       time.Time
	ReplyPosts int
	ReplyFiles int
}

func NewFormatter(text localization.Localizer) Formatter {
	return Formatter{text: text}
}

func (f Formatter) ThreadNotification(baseURL string, thread ThreadNotice, minReplyPosts int, now time.Time) telegram.OutgoingMessage {
	title := fmt.Sprintf("/%s/ #%d", thread.Board, thread.PostID)
	summary := f.notificationSummary(thread, minReplyPosts, now)
	url := fmt.Sprintf("%s/%s/thread/%d.html", strings.TrimRight(baseURL, "/"), thread.Board, thread.PostID)
	return telegram.MarkdownMessage(strings.Join([]string{
		"*" + title + "*",
		"_" + summary + "_",
		url,
	}, "\n"))
}

func (f Formatter) notificationSummary(thread ThreadNotice, minReplyPosts int, now time.Time) string {
	parts := []string{quantity(thread.ReplyPosts,
		f.text.Text(localization.TelegramReplyOne, "reply"),
		f.text.Text(localization.TelegramReplyMany, "replies"),
	)}
	if thread.ReplyFiles > 0 {
		parts = append(parts, quantity(thread.ReplyFiles,
			f.text.Text(localization.TelegramFileOne, "file"),
			f.text.Text(localization.TelegramFileMany, "files"),
		))
	}
	if label := f.thresholdReachedLabel(thread, minReplyPosts, now); label != "" {
		parts = append(parts, label)
	}
	return strings.Join(parts, ", ")
}

func (f Formatter) thresholdReachedLabel(thread ThreadNotice, minReplyPosts int, now time.Time) string {
	if minReplyPosts <= 1 || thread.Date.IsZero() || thread.ReplyPosts < minReplyPosts {
		return ""
	}

	elapsed := now.Sub(thread.Date)
	if elapsed < 0 {
		return ""
	}

	return f.text.Format(localization.TelegramThreshold, "hit %d in %s", minReplyPosts, humanizeElapsed(elapsed))
}

func quantity(count int, singular, plural string) string {
	if count == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %s", count, plural)
}

func humanizeElapsed(d time.Duration) string {
	if d < time.Hour {
		minutes := int(d.Round(time.Minute) / time.Minute)
		if minutes < 1 {
			minutes = 1
		}
		return fmt.Sprintf("%dm", minutes)
	}

	if d < 48*time.Hour {
		hours := int(d.Round(time.Hour) / time.Hour)
		if hours < 1 {
			hours = 1
		}
		return fmt.Sprintf("%dh", hours)
	}

	days := int(d.Round(24*time.Hour) / (24 * time.Hour))
	if days < 1 {
		days = 1
	}
	return fmt.Sprintf("%dd", days)
}
