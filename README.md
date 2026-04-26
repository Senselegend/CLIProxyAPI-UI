# CLI Proxy API

A proxy server that provides OpenAI/Gemini/Claude/Codex compatible API interfaces for CLI.

It now also supports OpenAI Codex (GPT models) and Claude Code via OAuth.

So you can use local or multi-account CLI access with OpenAI(include Responses)/Gemini/Claude-compatible clients and SDKs.

## Quick Setup for Codex and Claude Code

Russian version: [README_RU.md](README_RU.md)

This quick setup is specifically for using Codex/OpenAI accounts through CLIProxyAPI.

### Codex CLI

For Codex on macOS, set the token through `launchd` before starting the app:

```bash
launchctl setenv LOCALPROXY_API_KEY my-local-proxy-key
export LOCALPROXY_API_KEY=my-local-proxy-key
```

Then configure `~/.codex/config.toml`:

```toml
model = "gpt-5.4"
model_provider = "localproxy"

[model_providers.localproxy]
name = "Local proxy"
base_url = "http://localhost:8317/v1"
env_key = "LOCALPROXY_API_KEY"
wire_api = "responses"
```

Notes:
- `env_key` must be the environment variable name, not the token value itself.
- `wire_api = "responses"` is required for Codex CLI.
- After running `launchctl setenv ...`, fully restart the Codex app.

### Claude Code

Point Claude Code to the local proxy with this config:

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:8317",
    "ANTHROPIC_AUTH_TOKEN": "my-local-proxy-key",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "gpt-5.4-mini",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "gpt-5.4",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "gpt-5.4"
  },
  "model": "gpt-5.4"
}
```

Why configure it this way:
- `model` sets your default interactive model for normal GPT-backed usage.
- `ANTHROPIC_DEFAULT_HAIKU_MODEL`, `ANTHROPIC_DEFAULT_SONNET_MODEL`, and `ANTHROPIC_DEFAULT_OPUS_MODEL` keep Claude Code's internal model buckets mapped to the GPT models you want: Haiku → `gpt-5.4-mini`, Sonnet → `gpt-5.4`, Opus → `gpt-5.4`.
- This lets Claude Code keep using its normal model-selection behavior for agents and internal routing while still sending those choices through your proxy onto the GPT-backed models you prefer.

If you are using other providers through CLIProxyAPI and do not need the Codex/OpenAI model mapping, the default Claude Code config is just:

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:8317",
    "ANTHROPIC_AUTH_TOKEN": "my-local-proxy-key"
  }
}
```

## Overview

- OpenAI/Gemini/Claude compatible API endpoints for CLI models
- OpenAI Codex support (GPT models) via OAuth login
- Claude Code support via OAuth login
- Amp CLI and IDE extensions support with provider routing
- Streaming and non-streaming responses
- Function calling/tools support
- Multimodal input support (text and images)
- Multiple accounts with round-robin load balancing (Gemini, OpenAI, Claude)
- Simple CLI authentication flows (Gemini, OpenAI, Claude)
- Generative Language API Key support
- AI Studio Build multi-account load balancing
- Gemini CLI multi-account load balancing
- Claude Code multi-account load balancing
- OpenAI Codex multi-account load balancing
- OpenAI-compatible upstream providers via config (e.g., OpenRouter)
- Reusable Go SDK for embedding the proxy (see `docs/sdk-usage.md`)

## Getting Started

CLIProxyAPI Guides: [https://help.router-for.me/](https://help.router-for.me/)

## Management API

see [MANAGEMENT_API.md](https://help.router-for.me/management/api)

## Amp CLI Support

CLIProxyAPI includes integrated support for [Amp CLI](https://ampcode.com) and Amp IDE extensions, enabling you to use your Google/ChatGPT/Claude OAuth subscriptions with Amp's coding tools:

- Provider route aliases for Amp's API patterns (`/api/provider/{provider}/v1...`)
- Management proxy for OAuth authentication and account features
- Smart model fallback with automatic routing
- **Model mapping** to route unavailable models to alternatives (e.g., `claude-opus-4.5` → `claude-sonnet-4`)
- Security-first design with localhost-only management endpoints

When you need the request/response shape of a specific backend family, use the provider-specific paths instead of the merged `/v1/...` endpoints:

- Use `/api/provider/{provider}/v1/messages` for messages-style backends.
- Use `/api/provider/{provider}/v1beta/models/...` for model-scoped generate endpoints.
- Use `/api/provider/{provider}/v1/chat/completions` for chat-completions backends.

These routes help you select the protocol surface, but they do not by themselves guarantee a unique inference executor when the same client-visible model name is reused across multiple backends. Inference routing is still resolved from the request model/alias. For strict backend pinning, use unique aliases, prefixes, or otherwise avoid overlapping client-visible model names.

**→ [Complete Amp CLI Integration Guide](https://help.router-for.me/agent-client/amp-cli.html)**

## SDK Docs

- Usage: [docs/sdk-usage.md](docs/sdk-usage.md)
- Advanced (executors & translators): [docs/sdk-advanced.md](docs/sdk-advanced.md)
- Access: [docs/sdk-access.md](docs/sdk-access.md)
- Watcher: [docs/sdk-watcher.md](docs/sdk-watcher.md)
- Custom Provider Example: `examples/custom-provider`

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
