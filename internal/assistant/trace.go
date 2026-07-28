package assistant

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"martie/internal/deepseek"
)

type TraceConfig struct {
	Enabled  bool
	Dir      string
	MaxFiles int
}

type Trace struct {
	Surface          string
	StartedAt        time.Time
	MessageID        int64
	ThreadID         int64
	UserAlias        string
	UsedHistory      bool
	UsedReply        bool
	UsedPtchan       bool
	StoredBefore     []deepseek.Message
	SystemPrompt     string
	ModelMessages    []deepseek.Message
	Completion       deepseek.Completion
	Outcome          string
	Err              error
	StoredAfter      []deepseek.Message
	RemovedExchanges int
}

type TraceDumper struct {
	dir      string
	maxFiles int
}

func NewTraceDumper(cfg TraceConfig) *TraceDumper {
	if !cfg.Enabled {
		return nil
	}
	return &TraceDumper{dir: cfg.Dir, maxFiles: cfg.MaxFiles}
}

func (d *TraceDumper) Dump(trace *Trace) (string, error) {
	if err := os.MkdirAll(d.dir, 0700); err != nil {
		return "", fmt.Errorf("create trace directory: %w", err)
	}
	if err := os.Chmod(d.dir, 0700); err != nil {
		return "", fmt.Errorf("set trace directory permissions: %w", err)
	}

	surface := cleanTraceSurface(trace.Surface)
	name := fmt.Sprintf("martie-%s-%s-message-%d-*.trace", surface, trace.StartedAt.UTC().Format("20060102T150405.000000000Z"), trace.MessageID)
	file, err := os.CreateTemp(d.dir, name)
	if err != nil {
		return "", fmt.Errorf("create trace: %w", err)
	}
	path := file.Name()
	remove := true
	defer func() {
		file.Close()
		if remove {
			os.Remove(path)
		}
	}()
	if err := file.Chmod(0600); err != nil {
		return "", fmt.Errorf("set trace permissions: %w", err)
	}
	if _, err := file.WriteString(FormatTrace(trace)); err != nil {
		return "", fmt.Errorf("write trace: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close trace: %w", err)
	}
	remove = false
	if err := d.prune(surface); err != nil {
		return path, fmt.Errorf("prune traces: %w", err)
	}
	return path, nil
}

func (d *TraceDumper) prune(surface string) error {
	files, err := filepath.Glob(filepath.Join(d.dir, tracePattern(surface)))
	if err != nil {
		return err
	}
	if len(files) <= d.maxFiles {
		return nil
	}
	sort.Strings(files)
	for _, path := range files[:len(files)-d.maxFiles] {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

func tracePattern(surface string) string {
	return fmt.Sprintf("martie-%s-*.trace", cleanTraceSurface(surface))
}

func FormatTrace(trace *Trace) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ASSISTANT TRACE\nsurface: %s\nstarted_at: %s\nmessage_id: %d\ntelegram_thread_id: %d\nuser: %s\noutcome: %s\n\n", cleanTraceSurface(trace.Surface), trace.StartedAt.Format(time.RFC3339Nano), trace.MessageID, trace.ThreadID, trace.UserAlias, trace.Outcome)
	b.WriteString("CONTEXT DECISIONS\n")
	fmt.Fprintf(&b, "history: %s\nreply: %s\nptchan: %s\n\n", yesNo(trace.UsedHistory), yesNo(trace.UsedReply), yesNo(trace.UsedPtchan))
	writeTraceMessages(&b, "STORED BEFORE", trace.StoredBefore)
	b.WriteString("MODEL REQUEST\n\n")
	writeTraceContent(&b, "SYSTEM", trace.SystemPrompt)
	for i, message := range trace.ModelMessages {
		writeTraceContent(&b, fmt.Sprintf("MESSAGE %d | %s", i+1, message.Role), message.Content)
	}
	b.WriteString("MODEL RESULT\n")
	if trace.Err != nil {
		writeTraceContent(&b, "ERROR", trace.Err.Error())
	} else {
		fmt.Fprintf(&b, "finish_reason: %s\nprompt_tokens: %d\ncompletion_tokens: %d\ncache_hit_tokens: %d\ncache_miss_tokens: %d\n\n", trace.Completion.FinishReason, trace.Completion.Usage.PromptTokens, trace.Completion.Usage.CompletionTokens, trace.Completion.Usage.PromptCacheHitTokens, trace.Completion.Usage.PromptCacheMissTokens)
		writeTraceContent(&b, "RESPONSE", trace.Completion.Text)
	}
	fmt.Fprintf(&b, "removed_exchanges: %d\n\n", trace.RemovedExchanges)
	writeTraceMessages(&b, "STORED AFTER", trace.StoredAfter)
	return b.String()
}

func writeTraceMessages(b *strings.Builder, heading string, messages []deepseek.Message) {
	fmt.Fprintf(b, "%s\n", heading)
	if len(messages) == 0 {
		b.WriteString("(empty)\n\n")
		return
	}
	b.WriteByte('\n')
	for i, message := range messages {
		writeTraceContent(b, fmt.Sprintf("MESSAGE %d | %s", i+1, message.Role), message.Content)
	}
}

func writeTraceContent(b *strings.Builder, heading, content string) {
	fmt.Fprintf(b, "[%s | %d runes]\n", heading, len([]rune(content)))
	for _, line := range strings.Split(content, "\n") {
		fmt.Fprintf(b, "    %s\n", line)
	}
	b.WriteByte('\n')
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func cleanTraceSurface(surface string) string {
	surface = strings.TrimSpace(surface)
	if surface == "" {
		return "assistant"
	}
	cleaned := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, surface)
	cleaned = strings.Trim(cleaned, "-_")
	if cleaned == "" {
		return "assistant"
	}
	return cleaned
}
