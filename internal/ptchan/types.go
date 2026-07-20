package ptchan

import "time"

type Thread struct {
	Date       time.Time
	Board      string
	Name       string
	Subject    string
	Message    string
	ReplyPosts int
	ReplyFiles int
	Bumped     time.Time
	PostID     int64
	Tripcode   string
	Capcode    string
	Quotes     []Quote
	Replies    []Post
}

type Post struct {
	Date     time.Time
	Board    string
	Name     string
	Message  string
	ThreadID int64
	PostID   int64
	Tripcode string
	Capcode  string
	Quotes   []Quote
}

type Quote struct {
	ThreadID int64
	PostID   int64
}
