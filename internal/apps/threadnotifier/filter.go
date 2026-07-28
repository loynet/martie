package threadnotifier

import (
	"strings"
	"time"
)

type Filter struct {
	BoardDenylist   []string
	KeywordDenylist []string
	MaxThreadAge    time.Duration
}

type filterThread struct {
	Date    time.Time
	Board   string
	Subject string
	Message string
	PostID  int64
}

func (f Filter) allows(thread filterThread, now time.Time) bool {
	for _, board := range f.BoardDenylist {
		if strings.EqualFold(strings.TrimSpace(board), thread.Board) {
			return false
		}
	}

	text := strings.ToLower(strings.Join([]string{
		thread.Board,
		thread.Subject,
		thread.Message,
	}, "\n"))

	for _, keyword := range f.KeywordDenylist {
		keyword = strings.ToLower(strings.TrimSpace(keyword))
		if keyword != "" && strings.Contains(text, keyword) {
			return false
		}
	}

	return f.MaxThreadAge == 0 || thread.Date.IsZero() || now.Sub(thread.Date) <= f.MaxThreadAge
}
