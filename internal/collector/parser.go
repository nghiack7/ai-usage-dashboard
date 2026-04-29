package collector

import (
	"bytes"
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/personal/ai-usage-dashboard/internal/config"
	"github.com/personal/ai-usage-dashboard/internal/store"
)

type usageCandidate struct {
	input      int64
	output     int64
	cacheRead  int64
	cacheWrite int64
	total      int64
}

var tokenRegexes = []struct {
	target string
	re     *regexp.Regexp
}{
	{"input", regexp.MustCompile(`(?i)(?:input|prompt)[ _-]?tokens?\D+([0-9][0-9,]*)`)},
	{"output", regexp.MustCompile(`(?i)(?:output|completion)[ _-]?tokens?\D+([0-9][0-9,]*)`)},
	{"cache_read", regexp.MustCompile(`(?i)cache[ _-]?read[ _-]?(?:input[ _-]?)?tokens?\D+([0-9][0-9,]*)`)},
	{"cache_write", regexp.MustCompile(`(?i)(?:cache[ _-]?write|cache[ _-]?creation)[ _-]?(?:input[ _-]?)?tokens?\D+([0-9][0-9,]*)`)},
	{"total", regexp.MustCompile(`(?i)total[ _-]?tokens?\D+([0-9][0-9,]*)`)},
}

func parseLine(tool config.ToolConfig, path string, lineNo int, line string, fallbackTime time.Time) (store.Event, bool) {
	if line == "" {
		return store.Event{}, false
	}

	var raw any
	dec := json.NewDecoder(bytes.NewBufferString(line))
	dec.UseNumber()
	if err := dec.Decode(&raw); err == nil {
		return parseJSONValue(tool, path, lineNo, line, fallbackTime, raw)
	}
	return parseTextUsage(tool, path, lineNo, line, fallbackTime)
}

func parseJSONValue(tool config.ToolConfig, path string, lineNo int, line string, fallbackTime time.Time, raw any) (store.Event, bool) {
	candidate := bestUsage(raw)
	if candidate.total == 0 {
		return store.Event{}, false
	}
	occurred := findTime(raw)
	if occurred.IsZero() {
		occurred = fallbackTime
	}
	if occurred.IsZero() {
		occurred = time.Now()
	}
	rawKind := fallbackString(findString(raw, "type", "kind", "event", "eventtype"), "json")
	return store.Event{
		EventHash:        jsonEventHash(tool.Name, path, lineNo, line, rawKind, occurred, raw, candidate),
		ToolName:         tool.Name,
		SourcePath:       path,
		SourceLine:       lineNo,
		OccurredAt:       occurred.UTC(),
		Project:          fallbackString(findString(raw, "project", "projectname", "workspace", "workspacefolder", "cwd", "repo"), "unknown"),
		Model:            fallbackString(findString(raw, "model", "modelid", "modelname"), "unknown"),
		InputTokens:      candidate.input,
		OutputTokens:     candidate.output,
		CacheReadTokens:  candidate.cacheRead,
		CacheWriteTokens: candidate.cacheWrite,
		TotalTokens:      candidate.total,
		RawKind:          rawKind,
	}, true
}

func parseTextUsage(tool config.ToolConfig, path string, lineNo int, line string, fallbackTime time.Time) (store.Event, bool) {
	var usage usageCandidate
	for _, item := range tokenRegexes {
		matches := item.re.FindStringSubmatch(line)
		if len(matches) != 2 {
			continue
		}
		n, ok := parseIntLike(matches[1])
		if !ok {
			continue
		}
		switch item.target {
		case "input":
			usage.input = n
		case "output":
			usage.output = n
		case "cache_read":
			usage.cacheRead = n
		case "cache_write":
			usage.cacheWrite = n
		case "total":
			usage.total = n
		}
	}
	if usage.total == 0 {
		usage.total = usage.input + usage.output + usage.cacheRead + usage.cacheWrite
	}
	if usage.total == 0 {
		return store.Event{}, false
	}
	occurred := fallbackTime
	if occurred.IsZero() {
		occurred = time.Now()
	}
	return store.Event{
		EventHash:        eventHash(tool.Name, path, lineNo, line),
		ToolName:         tool.Name,
		SourcePath:       path,
		SourceLine:       lineNo,
		OccurredAt:       occurred.UTC(),
		Project:          "unknown",
		Model:            "unknown",
		InputTokens:      usage.input,
		OutputTokens:     usage.output,
		CacheReadTokens:  usage.cacheRead,
		CacheWriteTokens: usage.cacheWrite,
		TotalTokens:      usage.total,
		RawKind:          "text",
	}, true
}

func bestUsage(v any) usageCandidate {
	preferred := collectPreferredUsageCandidates(v)
	if len(preferred) > 0 {
		return largestUsage(preferred)
	}
	candidates := collectUsageCandidates(v)
	return largestUsage(candidates)
}

func largestUsage(candidates []usageCandidate) usageCandidate {
	var best usageCandidate
	for _, item := range candidates {
		if item.total == 0 {
			item.total = item.input + item.output + item.cacheRead + item.cacheWrite
		}
		if item.total > best.total {
			best = item
		}
	}
	return best
}

func collectPreferredUsageCandidates(v any) []usageCandidate {
	var out []usageCandidate
	switch typed := v.(type) {
	case map[string]any:
		for key, child := range typed {
			if normalizeKey(key) == "lasttokenusage" {
				out = append(out, directUsageCandidate(child))
				continue
			}
			out = append(out, collectPreferredUsageCandidates(child)...)
		}
	case []any:
		for _, child := range typed {
			out = append(out, collectPreferredUsageCandidates(child)...)
		}
	}
	return out
}

func collectUsageCandidates(v any) []usageCandidate {
	var out []usageCandidate
	switch typed := v.(type) {
	case map[string]any:
		candidate := directUsageCandidate(typed)
		if candidate.input+candidate.output+candidate.cacheRead+candidate.cacheWrite+candidate.total > 0 {
			if candidate.total == 0 {
				candidate.total = candidate.input + candidate.output + candidate.cacheRead + candidate.cacheWrite
			}
			out = append(out, candidate)
		}
		for _, child := range typed {
			out = append(out, collectUsageCandidates(child)...)
		}
	case []any:
		for _, child := range typed {
			out = append(out, collectUsageCandidates(child)...)
		}
	}
	return out
}

func directUsageCandidate(v any) usageCandidate {
	typed, ok := v.(map[string]any)
	if !ok {
		return usageCandidate{}
	}
	var candidate usageCandidate
	for key, value := range typed {
		n, ok := numberLike(value)
		if !ok {
			continue
		}
		switch normalizeKey(key) {
		case "inputtokens", "prompttokens":
			candidate.input += n
		case "outputtokens", "completiontokens":
			candidate.output += n
		case "cachedinputtokens", "cachereadtokens", "cachereadinputtokens":
			candidate.cacheRead += n
		case "cachewritetokens", "cachewriteinputtokens", "cachecreationtokens", "cachecreationinputtokens":
			candidate.cacheWrite += n
		case "totaltokens":
			candidate.total = maxInt64(candidate.total, n)
		}
	}
	if candidate.total == 0 {
		candidate.total = candidate.input + candidate.output + candidate.cacheRead + candidate.cacheWrite
	}
	return candidate
}

func jsonEventHash(toolName, path string, lineNo int, line, rawKind string, occurred time.Time, raw any, usage usageCandidate) string {
	stableID := stableJSONEventID(raw)
	if stableID == "" {
		return eventHash(toolName, path, lineNo, line)
	}
	payload := strings.Join([]string{
		toolName,
		stableID,
		rawKind,
		strconv.FormatInt(usage.input, 10),
		strconv.FormatInt(usage.output, 10),
		strconv.FormatInt(usage.cacheRead, 10),
		strconv.FormatInt(usage.cacheWrite, 10),
		strconv.FormatInt(usage.total, 10),
	}, "\x00")
	return hashString(payload)
}

func stableJSONEventID(v any) string {
	if s := stringAtPath(v, "requestId"); s != "" {
		return "request:" + s
	}
	if s := stringAtPath(v, "request_id"); s != "" {
		return "request:" + s
	}
	if s := stringAtPath(v, "message", "id"); s != "" {
		return "message:" + s
	}
	if s := stringAtPath(v, "response", "id"); s != "" {
		return "response:" + s
	}
	if s := stringAtPath(v, "payload", "id"); s != "" {
		return "payload:" + s
	}
	if s := stringAtPath(v, "id"); s != "" {
		return "id:" + s
	}
	if stringAtPath(v, "payload", "type") == "token_count" {
		if s := stringAtPath(v, "timestamp"); s != "" {
			return "codex-token-count:" + s
		}
	}
	return ""
}

func stringAtPath(v any, path ...string) string {
	current := v
	for _, key := range path {
		obj, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		var found any
		for objKey, value := range obj {
			if normalizeKey(objKey) == normalizeKey(key) {
				found = value
				break
			}
		}
		if found == nil {
			return ""
		}
		current = found
	}
	s, ok := current.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func findString(v any, keys ...string) string {
	wanted := map[string]struct{}{}
	for _, key := range keys {
		wanted[normalizeKey(key)] = struct{}{}
	}
	switch typed := v.(type) {
	case map[string]any:
		for key, value := range typed {
			if _, ok := wanted[normalizeKey(key)]; !ok {
				continue
			}
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
		for _, child := range typed {
			if s := findString(child, keys...); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range typed {
			if s := findString(child, keys...); s != "" {
				return s
			}
		}
	}
	return ""
}

func findTime(v any) time.Time {
	switch typed := v.(type) {
	case map[string]any:
		for key, value := range typed {
			switch normalizeKey(key) {
			case "timestamp", "createdat", "time", "ts", "datetime", "occurredat":
				if t := parseTimeLike(value); !t.IsZero() {
					return t
				}
			}
		}
		for _, child := range typed {
			if t := findTime(child); !t.IsZero() {
				return t
			}
		}
	case []any:
		for _, child := range typed {
			if t := findTime(child); !t.IsZero() {
				return t
			}
		}
	}
	return time.Time{}
}

func parseTimeLike(v any) time.Time {
	switch typed := v.(type) {
	case string:
		value := strings.TrimSpace(typed)
		if value == "" {
			return time.Time{}
		}
		layouts := []string{
			time.RFC3339Nano,
			time.RFC3339,
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05.000Z07:00",
		}
		for _, layout := range layouts {
			if t, err := time.Parse(layout, value); err == nil {
				return t
			}
		}
		if n, ok := parseIntLike(value); ok {
			return unixTime(n)
		}
	case json.Number:
		if n, err := typed.Int64(); err == nil {
			return unixTime(n)
		}
	case float64:
		return unixTime(int64(typed))
	}
	return time.Time{}
}

func unixTime(n int64) time.Time {
	switch {
	case n <= 0:
		return time.Time{}
	case n > 1_000_000_000_000:
		return time.UnixMilli(n).UTC()
	case n > 1_000_000_000:
		return time.Unix(n, 0).UTC()
	default:
		return time.Time{}
	}
}

func normalizeKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	replacer := strings.NewReplacer("_", "", "-", "", ".", "", " ", "")
	return replacer.Replace(key)
}

func numberLike(v any) (int64, bool) {
	switch typed := v.(type) {
	case json.Number:
		if n, err := typed.Int64(); err == nil {
			return n, true
		}
		if f, err := typed.Float64(); err == nil {
			return int64(math.Round(f)), true
		}
	case float64:
		return int64(math.Round(typed)), true
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case string:
		return parseIntLike(typed)
	}
	return 0, false
}

func parseIntLike(value string) (int64, bool) {
	value = strings.ReplaceAll(strings.TrimSpace(value), ",", "")
	if value == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
