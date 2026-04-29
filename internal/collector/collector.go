package collector

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/personal/ai-usage-dashboard/internal/config"
	"github.com/personal/ai-usage-dashboard/internal/store"
)

type Collector struct {
	cfg config.Config
	db  *store.Store
}

type Result struct {
	FilesSeen      int64 `json:"files_seen"`
	EventsSeen     int64 `json:"events_seen"`
	EventsInserted int64 `json:"events_inserted"`
}

func New(cfg config.Config, db *store.Store) *Collector {
	return &Collector{cfg: cfg, db: db}
}

func (c *Collector) Scan(ctx context.Context) (Result, error) {
	scanID, err := c.db.StartScan(ctx)
	if err != nil {
		return Result{}, err
	}

	result, scanErr := c.scan(ctx)
	status := "ok"
	if scanErr != nil {
		status = "error"
	}
	if finishErr := c.db.FinishScan(ctx, scanID, status, result.FilesSeen, result.EventsSeen, result.EventsInserted, scanErr); finishErr != nil {
		if scanErr != nil {
			return result, errors.Join(scanErr, finishErr)
		}
		return result, finishErr
	}
	return result, scanErr
}

func (c *Collector) scan(ctx context.Context) (Result, error) {
	var result Result
	var errs []error

	for _, tool := range c.cfg.Tools {
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("scan canceled: %w", err)
		}
		if !tool.Enabled {
			continue
		}
		if err := c.db.UpsertTool(ctx, store.Tool{
			Name:               tool.Name,
			DisplayName:        tool.DisplayName,
			Enabled:            tool.Enabled,
			MonthlyCostUSD:     tool.MonthlyCostUSD,
			MonthlyQuotaTokens: tool.MonthlyQuotaTokens,
		}); err != nil {
			errs = append(errs, err)
			continue
		}

		paths, err := expandLogPaths(tool.LogPaths)
		if err != nil {
			errs = append(errs, fmt.Errorf("expand log paths for %s: %w", tool.Name, err))
			continue
		}
		for _, path := range paths {
			if err := ctx.Err(); err != nil {
				return result, fmt.Errorf("scan canceled: %w", err)
			}
			fileResult, err := c.scanFile(ctx, tool, path)
			result.FilesSeen += fileResult.FilesSeen
			result.EventsSeen += fileResult.EventsSeen
			result.EventsInserted += fileResult.EventsInserted
			if err != nil {
				errs = append(errs, err)
			}
		}
	}
	return result, errors.Join(errs...)
}

func (c *Collector) scanFile(ctx context.Context, tool config.ToolConfig, path string) (Result, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Result{}, nil
		}
		return Result{}, fmt.Errorf("stat %q: %w", path, err)
	}
	if info.IsDir() {
		return Result{}, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return Result{}, fmt.Errorf("open %q: %w", path, err)
	}
	defer f.Close()

	state, err := c.db.GetFileState(ctx, path, info.Size())
	if err != nil {
		return Result{}, err
	}
	if state.Offset > 0 {
		if _, err := f.Seek(state.Offset, io.SeekStart); err != nil {
			return Result{}, fmt.Errorf("seek %q to offset %d: %w", path, state.Offset, err)
		}
	}

	result := Result{FilesSeen: 1}
	reader := bufio.NewReaderSize(f, 1024*1024)
	lineNo := state.LineCount
	for {
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			lineNo++
			ev, ok := parseLine(tool, path, lineNo, strings.TrimSpace(line), info.ModTime())
			if ok {
				result.EventsSeen++
				inserted, err := c.db.InsertEvent(ctx, ev)
				if err != nil {
					return result, err
				}
				if inserted {
					result.EventsInserted++
				}
			}
		}
		if readErr == nil {
			continue
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		return result, fmt.Errorf("read %q line %d: %w", path, lineNo+1, readErr)
	}
	offset, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return result, fmt.Errorf("read current offset for %q: %w", path, err)
	}
	if err := c.db.SaveFileState(ctx, store.FileState{
		SourcePath: path,
		SizeBytes:  info.Size(),
		ModTime:    info.ModTime(),
		Offset:     offset,
		LineCount:  lineNo,
	}); err != nil {
		return result, err
	}
	return result, nil
}

func expandLogPaths(patterns []string) ([]string, error) {
	seen := map[string]struct{}{}
	var out []string
	for _, pattern := range patterns {
		expanded := config.ExpandPath(pattern)
		matches, err := expandPattern(expanded)
		if err != nil {
			return nil, fmt.Errorf("glob %q: %w", pattern, err)
		}
		if len(matches) == 0 {
			matches = []string{expanded}
		}
		for _, match := range matches {
			clean := filepath.Clean(match)
			if _, exists := seen[clean]; exists {
				continue
			}
			seen[clean] = struct{}{}
			out = append(out, clean)
		}
	}
	return out, nil
}

func expandPattern(pattern string) ([]string, error) {
	if !strings.Contains(pattern, "**") {
		return filepath.Glob(pattern)
	}

	idx := strings.Index(pattern, "**")
	root := filepath.Clean(strings.TrimSuffix(pattern[:idx], string(os.PathSeparator)))
	if root == "" || root == "." {
		root = "."
	}
	suffix := strings.TrimPrefix(pattern[idx+2:], string(os.PathSeparator))
	suffix = filepath.ToSlash(suffix)

	var matches []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if suffix == "" {
			matches = append(matches, path)
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		target := filepath.ToSlash(rel)
		if !strings.Contains(suffix, "/") {
			target = filepath.Base(target)
		}
		ok, err := filepath.Match(suffix, target)
		if err != nil {
			return err
		}
		if ok {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return matches, nil
}

func eventHash(toolName, path string, lineNo int, line string) string {
	return hashString(fmt.Sprintf("%s\x00%s\x00%d\x00%s", toolName, path, lineNo, line))
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
