package ptchan

import "testing"

func TestParseThreadLink(t *testing.T) {
	link, ok := ParseThreadLink("https://ptchan.org/i/thread/303160.html#303241", "https://ptchan.org")
	if !ok {
		t.Fatal("link was not parsed")
	}
	if link.Board != "i" || link.ThreadID != 303160 || link.PostID != 303241 {
		t.Fatalf("link = %+v", link)
	}
}

func TestParseThreadLinkIgnoresOtherHosts(t *testing.T) {
	_, ok := ParseThreadLink("https://example.com/i/thread/303160.html", "https://ptchan.org")
	if ok {
		t.Fatal("foreign host was accepted")
	}
}
