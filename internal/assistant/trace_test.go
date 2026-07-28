package assistant

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"martie/internal/deepseek"
)

func TestFormatAssistantTraceSeparatesStoredAndModelContext(t *testing.T) {
	trace := &Trace{
		Surface:      "chatter",
		StartedAt:    time.Date(2026, time.July, 11, 12, 24, 43, 0, time.UTC),
		MessageID:    42,
		ThreadID:     7,
		UserAlias:    "@assistant_user_local_0001",
		UsedHistory:  true,
		UsedReply:    true,
		UsedPtchan:   true,
		StoredBefore: []deepseek.Message{{Role: deepseek.RoleUser, Content: "old request"}},
		SystemPrompt: "system\nprompt",
		ModelMessages: []deepseek.Message{
			{Role: deepseek.RoleUser, Content: "old request"},
			{Role: deepseek.RoleUser, Content: "BEGIN PTCHAN CONTEXT\nuntrusted\nEND PTCHAN CONTEXT\n\nUser request:\nexplain"},
		},
		Completion:  deepseek.Completion{Text: "answer", FinishReason: deepseek.FinishStop},
		Outcome:     "stored",
		StoredAfter: []deepseek.Message{{Role: deepseek.RoleUser, Content: "explain"}, {Role: deepseek.RoleAssistant, Content: "answer"}},
	}

	got := FormatTrace(trace)
	for _, want := range []string{
		"surface: chatter",
		"CONTEXT DECISIONS\nhistory: yes\nreply: yes\nptchan: yes",
		"STORED BEFORE\n\n[MESSAGE 1 | user | 11 runes]\n    old request",
		"MODEL REQUEST\n\n[SYSTEM | 13 runes]\n    system\n    prompt",
		"    BEGIN PTCHAN CONTEXT\n    untrusted",
		"MODEL RESULT\nfinish_reason: stop",
		"STORED AFTER\n\n[MESSAGE 1 | user | 7 runes]\n    explain",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("trace missing %q:\n%s", want, got)
		}
	}
}

func TestAssistantTraceDumperWritesPrivateFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "traces")
	dumper := NewTraceDumper(TraceConfig{Enabled: true, Dir: dir, MaxFiles: 100})
	path, err := dumper.Dump(&Trace{
		Surface:   "chatter",
		StartedAt: time.Date(2026, time.July, 11, 12, 24, 43, 0, time.UTC),
		MessageID: 42,
		Outcome:   "stored",
	})
	if err != nil {
		t.Fatalf("dump() error = %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	if !strings.Contains(string(contents), "message_id: 42") {
		t.Fatalf("trace contents = %q", contents)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat trace: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("trace permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestAssistantTraceDumperPrunesOldestFiles(t *testing.T) {
	dir := t.TempDir()
	dumper := NewTraceDumper(TraceConfig{Enabled: true, Dir: dir, MaxFiles: 2})
	unrelated := filepath.Join(dir, "unrelated.trace")
	if err := os.WriteFile(unrelated, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	startedAt := time.Date(2026, time.July, 11, 12, 24, 43, 0, time.UTC)
	for i := range 3 {
		if _, err := dumper.Dump(&Trace{Surface: "chatter", StartedAt: startedAt.Add(time.Duration(i) * time.Second), MessageID: int64(i + 1)}); err != nil {
			t.Fatalf("dump trace %d: %v", i, err)
		}
	}

	files, err := filepath.Glob(filepath.Join(dir, tracePattern("chatter")))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("trace files = %v, want newest two", files)
	}
	if strings.Contains(filepath.Base(files[0]), "message-1-") || strings.Contains(filepath.Base(files[1]), "message-1-") {
		t.Fatalf("oldest trace was not pruned: %v", files)
	}
	if contents, err := os.ReadFile(unrelated); err != nil || string(contents) != "keep" {
		t.Fatalf("unrelated trace was changed: contents = %q, error = %v", contents, err)
	}
}

func TestAssistantTraceDumperPrunesOnlyCurrentSurface(t *testing.T) {
	dir := t.TempDir()
	dumper := NewTraceDumper(TraceConfig{Enabled: true, Dir: dir, MaxFiles: 2})
	startedAt := time.Date(2026, time.July, 11, 12, 24, 43, 0, time.UTC)

	for i := range 3 {
		if _, err := dumper.Dump(&Trace{Surface: "chatter", StartedAt: startedAt.Add(time.Duration(i) * time.Second), MessageID: int64(i + 1)}); err != nil {
			t.Fatalf("dump chatter trace %d: %v", i, err)
		}
		if _, err := dumper.Dump(&Trace{Surface: "channer", StartedAt: startedAt.Add(time.Duration(i) * time.Second), MessageID: int64(i + 1)}); err != nil {
			t.Fatalf("dump channer trace %d: %v", i, err)
		}
	}

	chatterFiles, err := filepath.Glob(filepath.Join(dir, tracePattern("chatter")))
	if err != nil {
		t.Fatal(err)
	}
	channerFiles, err := filepath.Glob(filepath.Join(dir, tracePattern("channer")))
	if err != nil {
		t.Fatal(err)
	}
	if len(chatterFiles) != 2 || len(channerFiles) != 2 {
		t.Fatalf("trace files = chatter:%v channer:%v, want two per surface", chatterFiles, channerFiles)
	}
}

func TestNewAssistantTraceDumperDisabled(t *testing.T) {
	if got := NewTraceDumper(TraceConfig{}); got != nil {
		t.Fatalf("NewTraceDumper() = %+v, want nil", got)
	}
}
