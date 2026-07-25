package assistant

import (
	"strings"
	"unicode/utf8"
)

func TruncateRunes(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit-1]) + "…"
}

func WriteFencedBlock(b *strings.Builder, info, body string) {
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
	longest := 0
	current := 0
	for _, r := range text {
		if r == '`' {
			current++
			if current > longest {
				longest = current
			}
			continue
		}
		current = 0
	}
	return longest
}
