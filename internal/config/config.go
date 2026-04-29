package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Server ServerConfig `json:"server"`
	Scan   ScanConfig   `json:"scan"`
	Tools  []ToolConfig `json:"tools"`
}

type ServerConfig struct {
	Addr string `json:"addr"`
}

type ScanConfig struct {
	IntervalSeconds int `json:"interval_seconds"`
}

type ToolConfig struct {
	Name               string   `json:"name"`
	DisplayName        string   `json:"display_name"`
	Enabled            bool     `json:"enabled"`
	MonthlyCostUSD     float64  `json:"monthly_cost_usd"`
	MonthlyQuotaTokens int64    `json:"monthly_quota_tokens"`
	LogPaths           []string `json:"log_paths"`
	Parser             string   `json:"parser"`
}

func Load(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		return Config{}, errors.New("config path is required")
	}

	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config %q: %w", path, err)
	}
	defer f.Close()

	cfg := Default()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config %q: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config %q: %w", path, err)
	}
	return cfg, nil
}

func Default() Config {
	return Config{
		Server: ServerConfig{Addr: ":8080"},
		Scan:   ScanConfig{IntervalSeconds: int((5 * time.Minute).Seconds())},
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Server.Addr) == "" {
		return errors.New("server.addr must not be empty")
	}
	if c.Scan.IntervalSeconds < 0 {
		return errors.New("scan.interval_seconds must be >= 0")
	}
	seen := make(map[string]struct{}, len(c.Tools))
	for i, tool := range c.Tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			return fmt.Errorf("tools[%d].name must not be empty", i)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate tool name %q", name)
		}
		seen[name] = struct{}{}
		if tool.MonthlyCostUSD < 0 {
			return fmt.Errorf("tools[%d].monthly_cost_usd must be >= 0", i)
		}
		if tool.MonthlyQuotaTokens < 0 {
			return fmt.Errorf("tools[%d].monthly_quota_tokens must be >= 0", i)
		}
	}
	return nil
}

func ExpandPath(pattern string) string {
	pattern = os.ExpandEnv(pattern)
	if pattern == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(pattern, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(pattern, "~/"))
		}
	}
	return pattern
}
