package ptchan

import (
	"net/url"
	"strconv"
	"strings"
)

type ThreadLink struct {
	Board    string
	ThreadID int64
	PostID   int64
}

func ParseThreadLink(raw, baseURL string) (ThreadLink, bool) {
	host := "ptchan.org"
	if parsed, err := url.Parse(baseURL); err == nil && parsed.Host != "" {
		host = strings.ToLower(parsed.Host)
	}

	parsed, err := url.Parse(strings.TrimRight(raw, ".,;:!?)]}"))
	if err != nil || parsed.Scheme == "" || !strings.EqualFold(parsed.Host, host) {
		return ThreadLink{}, false
	}

	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 3 || parts[1] != "thread" {
		return ThreadLink{}, false
	}
	threadID, err := strconv.ParseInt(strings.TrimSuffix(parts[2], ".html"), 10, 64)
	if err != nil || threadID <= 0 || parts[0] == "" {
		return ThreadLink{}, false
	}
	var postID int64
	if parsed.Fragment != "" {
		postID, _ = strconv.ParseInt(strings.TrimPrefix(parsed.Fragment, "p"), 10, 64)
	}
	return ThreadLink{Board: parts[0], ThreadID: threadID, PostID: postID}, true
}
