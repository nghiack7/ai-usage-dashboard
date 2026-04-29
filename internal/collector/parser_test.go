package collector

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/personal/ai-usage-dashboard/internal/config"
)

func TestParseJSONUsage(t *testing.T) {
	line := `{"timestamp":"2026-04-29T10:00:00Z","type":"message","message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":1200,"output_tokens":340,"cache_read_input_tokens":500,"cache_creation_input_tokens":20}},"cwd":"/repo"}`
	ev, ok := parseLine(config.ToolConfig{Name: "claude"}, "/tmp/session.jsonl", 7, line, time.Time{})
	if !ok {
		t.Fatal("expected usage event")
	}
	if ev.InputTokens != 1200 || ev.OutputTokens != 340 || ev.CacheReadTokens != 500 || ev.CacheWriteTokens != 20 {
		t.Fatalf("unexpected tokens: %+v", ev)
	}
	if ev.TotalTokens != 2060 {
		t.Fatalf("unexpected total tokens: %d", ev.TotalTokens)
	}
	if ev.Model != "claude-sonnet-4-5" {
		t.Fatalf("unexpected model: %q", ev.Model)
	}
	if ev.Project != "/repo" {
		t.Fatalf("unexpected project: %q", ev.Project)
	}
}

func TestParseClaudeDuplicateMessageRowsUseSameEventHash(t *testing.T) {
	base := `"parentUuid":"x","isSidechain":false,"message":{"model":"claude-opus-4-7","id":"msg_same","type":"message","role":"assistant","usage":{"input_tokens":6,"cache_creation_input_tokens":1058,"cache_read_input_tokens":765021,"output_tokens":247}},"requestId":"req_same","type":"assistant","cwd":"/repo","sessionId":"session"`
	line1 := `{` + base + `,"uuid":"row1","timestamp":"2026-04-19T08:44:55.240Z"}`
	line2 := `{` + base + `,"uuid":"row2","timestamp":"2026-04-19T08:44:56.865Z"}`
	ev1, ok := parseLine(config.ToolConfig{Name: "claude"}, "/tmp/session.jsonl", 1, line1, time.Time{})
	if !ok {
		t.Fatal("expected first usage event")
	}
	ev2, ok := parseLine(config.ToolConfig{Name: "claude"}, "/tmp/session.jsonl", 2, line2, time.Time{})
	if !ok {
		t.Fatal("expected second usage event")
	}
	if ev1.EventHash != ev2.EventHash {
		t.Fatalf("expected duplicate message rows to share hash, got %s and %s", ev1.EventHash, ev2.EventHash)
	}
}

func TestParseOpenAIStyleUsage(t *testing.T) {
	line := `{"created_at":1777456800000,"model":"gpt-5.4","usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150}}`
	ev, ok := parseLine(config.ToolConfig{Name: "codex"}, "/tmp/codex.jsonl", 1, line, time.Time{})
	if !ok {
		t.Fatal("expected usage event")
	}
	if ev.InputTokens != 100 || ev.OutputTokens != 50 || ev.TotalTokens != 150 {
		t.Fatalf("unexpected usage: %+v", ev)
	}
	if ev.OccurredAt.Year() != 2026 {
		t.Fatalf("expected unix millis timestamp to parse, got %s", ev.OccurredAt)
	}
}

func TestParseCodexPrefersLastTokenUsageOverCumulativeTotal(t *testing.T) {
	line := `{"timestamp":"2026-03-13T06:12:54.251Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":275225234,"cached_input_tokens":258366592,"output_tokens":888602,"total_tokens":276113836},"last_token_usage":{"input_tokens":96575,"cached_input_tokens":65920,"output_tokens":889,"total_tokens":97464},"model_context_window":258400},"plan_type":"plus"}}`
	ev, ok := parseLine(config.ToolConfig{Name: "codex"}, "/tmp/codex.jsonl", 1, line, time.Time{})
	if !ok {
		t.Fatal("expected usage event")
	}
	if ev.InputTokens != 96575 || ev.OutputTokens != 889 || ev.CacheReadTokens != 65920 || ev.TotalTokens != 97464 {
		t.Fatalf("unexpected codex usage: %+v", ev)
	}
}

func TestParseCodexTokenCountCopiedAcrossPathsUsesSameEventHash(t *testing.T) {
	line := `{"timestamp":"2026-03-12T06:32:48.311Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":62613132,"cached_input_tokens":59327616,"output_tokens":183728,"total_tokens":62796860},"last_token_usage":{"input_tokens":238788,"cached_input_tokens":18816,"output_tokens":5676,"total_tokens":244464},"model_context_window":258400},"plan_type":"plus"}}`
	ev1, ok := parseLine(config.ToolConfig{Name: "codex"}, "/tmp/sessions/a.jsonl", 10, line, time.Time{})
	if !ok {
		t.Fatal("expected first usage event")
	}
	ev2, ok := parseLine(config.ToolConfig{Name: "codex"}, "/tmp/archived_sessions/a.jsonl", 11, line, time.Time{})
	if !ok {
		t.Fatal("expected second usage event")
	}
	if ev1.EventHash != ev2.EventHash {
		t.Fatalf("expected copied token_count rows to share hash, got %s and %s", ev1.EventHash, ev2.EventHash)
	}
}

func TestParseJSONDoesNotFallbackToTextInsideCommandOutput(t *testing.T) {
	line := `{"timestamp":"2026-04-29T02:19:05.023Z","type":"event_msg","payload":{"type":"exec_command_end","aggregated_output":"{\"totals\":{\"input_tokens\":194211749762,\"output_tokens\":971148128,\"total_tokens\":197625939915}}"}}`
	_, ok := parseLine(config.ToolConfig{Name: "codex"}, "/tmp/codex.jsonl", 1, line, time.Time{})
	if ok {
		t.Fatal("expected command output JSON to be ignored")
	}
}

func TestParseTextUsage(t *testing.T) {
	fallback := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	line := `request done input tokens: 1,500 output_tokens=250 total tokens 1750`
	ev, ok := parseLine(config.ToolConfig{Name: "antigravity"}, "/tmp/app.log", 3, line, fallback)
	if !ok {
		t.Fatal("expected text usage event")
	}
	if ev.InputTokens != 1500 || ev.OutputTokens != 250 || ev.TotalTokens != 1750 {
		t.Fatalf("unexpected text usage: %+v", ev)
	}
}

func TestExpandLogPathsSupportsRecursiveGlob(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(nested, "session.jsonl")
	if err := os.WriteFile(want, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skip.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := expandLogPaths([]string{filepath.Join(root, "**", "*.jsonl")})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("expected %q, got %#v", want, got)
	}
}
