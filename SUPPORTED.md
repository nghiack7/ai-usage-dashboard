# Supported Tools And Models

AI Usage Dashboard is model-agnostic. It does not whitelist model names.

If a configured log source includes a model field such as `model`, `model_id`, or `modelName`, the dashboard records and displays that value. If no model field is available, the event is stored with `unknown`.

## Out-Of-The-Box Tool Presets

The default config includes read-only log path presets for:

| Tool | Status | Notes |
| --- | --- | --- |
| Codex | Supported | Parses JSONL session logs and token count events. |
| Claude Code | Supported | Parses Claude project/session JSONL usage records. |
| Antigravity | Best effort | Parses plain text or JSON logs when token fields are present. |
| GodClaude | Best effort | Parses configured JSONL/text logs when token fields are present. |

## Model Coverage

The dashboard supports any model emitted by those tools, including provider families such as:

- OpenAI / Codex models, when recorded in local logs.
- Anthropic Claude models, when recorded in local logs.
- Google Gemini models, when used through a tool that records usage fields.
- xAI Grok models, when used through a tool that records usage fields.
- DeepSeek models, when used through a tool that records usage fields.
- Qwen models, when used through a tool that records usage fields.
- Llama models, when used through a tool that records usage fields.
- Mistral models, when used through a tool that records usage fields.
- Any local or custom model name present in logs.

This project intentionally avoids hardcoding a "hot model" list because model names and availability change quickly. The stable contract is the usage schema, not the model brand.

## Required Usage Fields

A log event can be parsed when it contains one or more common token fields:

- `input_tokens`
- `prompt_tokens`
- `output_tokens`
- `completion_tokens`
- `cache_read_input_tokens`
- `cached_input_tokens`
- `cache_creation_input_tokens`
- `cache_write_input_tokens`
- `total_tokens`

JSONL is preferred. Plain text logs are supported best-effort when they contain recognizable token labels.

## Adding Another AI Tool

Add a tool entry to `config.example.json` or your own `config.json`:

```json
{
  "name": "my-ai-tool",
  "display_name": "My AI Tool",
  "enabled": true,
  "monthly_cost_usd": 20,
  "monthly_quota_tokens": 0,
  "parser": "generic-jsonl",
  "log_paths": [
    "/host/path/to/my-ai-tool/**/*.jsonl"
  ]
}
```

If the log format is unsupported, add a small redacted sample to an issue or extend `internal/collector/parser.go`.

