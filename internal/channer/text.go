package channer

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func truncateRunes(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit-1]) + "…"
}

func stripUnsafeControls(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, text)
}

func writeFencedBlock(b *strings.Builder, info, body string) {
	fence := strings.Repeat("`", longestBacktickRun(body)+1)
	if len(fence) < 3 {
		fence = "```"
	}
	b.WriteString(fence)
	if info != "" {
		b.WriteString(info)
	}
	b.WriteByte('\n')
	b.WriteString(body)
	b.WriteByte('\n')
	b.WriteString(fence)
}

func longestBacktickRun(text string) int {
	longest, current := 0, 0
	for _, r := range text {
		if r == '`' {
			current++
			if current > longest {
				longest = current
			}
		} else {
			current = 0
		}
	}
	return longest
}
