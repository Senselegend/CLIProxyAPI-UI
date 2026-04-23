# CLI Proxy API

CLIProxyAPI — это локальный прокси-сервер, который предоставляет OpenAI/Gemini/Claude/Codex-совместимые API для CLI-инструментов.

Проект также поддерживает OpenAI Codex (GPT-модели) и Claude Code через OAuth.

Это позволяет использовать локальный доступ или несколько аккаунтов одновременно в CLI и SDK, совместимых с OpenAI (включая Responses API), Gemini и Claude.

## Быстрая настройка Codex и Claude Code

English version: [README.md](README.md)

Этот раздел относится именно к сценарию, где вы используете Codex/OpenAI-аккаунты через CLIProxyAPI.

Если вы хотите включить Fast-режим для GPT/Codex-модели через CLIProxyAPI, добавьте Fast-алиас в `config.yaml`, а затем настройте клиент по примерам ниже.

> [!ATTENTION]
> Fast-режим расходует примерно в 2 раза больше лимитов и токенов по сравнению с обычным tier.

Чтобы добавить глобально доступный алиас для Fast-режима, укажите в `config.yaml`:

```yaml
oauth-model-alias:
  codex:
    - name: "gpt-5.4"
      alias: "Codex-5.4-Fast"
      fork: true
```

Такая конфигурация сохраняет исходную модель `gpt-5.4` и добавляет алиас `Codex-5.4-Fast`, который маршрутизируется в ту же upstream-модель, но с `service_tier=fast` для Codex Responses-запросов.

### Codex CLI

На macOS перед запуском приложения передайте токен через `launchd`:

```bash
launchctl setenv LOCALPROXY_API_KEY my-local-proxy-key
export LOCALPROXY_API_KEY=my-local-proxy-key
```

Затем настройте `~/.codex/config.toml`:

```toml
model = "Codex-5.4"
model_provider = "localproxy"

[model_providers.localproxy]
name = "Local proxy"
base_url = "http://localhost:8317/v1"
env_key = "LOCALPROXY_API_KEY"
wire_api = "responses"
```

Если Fast-режим должен использоваться по умолчанию, укажите `model = "Codex-5.4-Fast"`.

Примечания:
- `env_key` должен содержать имя переменной окружения, а не сам токен.
- Для Codex CLI обязателен `wire_api = "responses"`.
- После `launchctl setenv ...` полностью перезапустите приложение Codex.

### Claude Code

Для работы через локальный прокси добавьте в `.claude/settings.json`:

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:8317",
    "ANTHROPIC_AUTH_TOKEN": "my-local-proxy-key",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "gpt-5.4-mini",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "gpt-5.4",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "gpt-5.4"
  },
  "model": "Codex-5.4-Fast"
}
```

Зачем это нужно:
- Поле `model` задаёт модель по умолчанию для интерактивной работы. Если указать `Codex-5.4-Fast`, Fast-режим станет основным пресетом.
- Переменные `ANTHROPIC_DEFAULT_HAIKU_MODEL`, `ANTHROPIC_DEFAULT_SONNET_MODEL` и `ANTHROPIC_DEFAULT_OPUS_MODEL` переопределяют внутренние модельные профили Claude Code и направляют их на нужные GPT-модели: Haiku → `gpt-5.4-mini`, Sonnet → `gpt-5.4`, Opus → `gpt-5.4`.
- Благодаря этому Claude Code сохраняет свою стандартную логику выбора моделей для агентов и внутренних вызовов, но фактически отправляет запросы через ваш прокси на выбранные GPT-модели.
- Если Fast-режим по умолчанию не нужен, просто удалите строку `"model": "Codex-5.4-Fast"` и оставьте только mapping через `ANTHROPIC_DEFAULT_*_MODEL`.

Если вы используете другие провайдеры через CLIProxyAPI и вам не нужен Fast setup для Codex/OpenAI, базовая конфигурация Claude Code выглядит так:

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:8317",
    "ANTHROPIC_AUTH_TOKEN": "my-local-proxy-key"
  }
}
```

## Обзор

- OpenAI/Gemini/Claude-совместимые API endpoint’ы для CLI-инструментов
- Поддержка OpenAI Codex (GPT-моделей) через OAuth-авторизацию
- Поддержка Claude Code через OAuth-авторизацию
- Поддержка Amp CLI и IDE-расширений с маршрутизацией по провайдерам
- Поддержка streaming и non-streaming ответов
- Поддержка function calling / tools
- Поддержка мультимодального ввода (текст и изображения)
- Поддержка нескольких аккаунтов с балансировкой нагрузки round-robin (Gemini, OpenAI, Claude)
- Простые CLI-сценарии аутентификации для Gemini, OpenAI и Claude
- Поддержка Generative Language API Key
- Балансировка нагрузки между несколькими аккаунтами AI Studio Build
- Балансировка нагрузки между несколькими аккаунтами Gemini CLI
- Балансировка нагрузки между несколькими аккаунтами Claude Code
- Балансировка нагрузки между несколькими аккаунтами OpenAI Codex
- Поддержка OpenAI-совместимых upstream-провайдеров через конфиг (например, OpenRouter)
- Повторно используемый Go SDK для встраивания прокси в другие Go-приложения (см. `docs/sdk-usage.md`)

## Начало работы

Руководства по CLIProxyAPI: [https://help.router-for.me/](https://help.router-for.me/)

## Management API

См. [MANAGEMENT_API.md](https://help.router-for.me/management/api)

## Поддержка Amp CLI

CLIProxyAPI содержит встроенную поддержку [Amp CLI](https://ampcode.com) и Amp IDE Extensions, что позволяет использовать ваши OAuth-подписки Google / ChatGPT / Claude в инструментах разработки Amp:

- алиасы provider routes для API-паттернов Amp (`/api/provider/{provider}/v1...`)
- management proxy для OAuth-аутентификации и операций с аккаунтами
- автоматический fallback моделей и маршрутизация
- **model mapping** для перенаправления недоступных моделей на альтернативы (например, `claude-opus-4.5` → `claude-sonnet-4`)
- security-first архитектура с management endpoint’ами, доступными только с localhost

Если вам нужна точная форма запросов и ответов конкретного backend-семейства, используйте provider-specific маршруты вместо общих `/v1/...` endpoint’ов:

- `/api/provider/{provider}/v1/messages` — для backends с messages-style API
- `/api/provider/{provider}/v1beta/models/...` — для model-scoped generate endpoint’ов
- `/api/provider/{provider}/v1/chat/completions` — для backends с chat-completions API

Эти маршруты помогают выбрать нужную protocol surface, но сами по себе не гарантируют, что будет использован строго один inference executor, если одно и то же клиентское имя модели пересекается между несколькими backend’ами. Итоговая маршрутизация по-прежнему определяется моделью или алиасом из запроса. Если вам нужен жёсткий backend pinning, используйте уникальные алиасы, префиксы или другие способы избежать пересечения client-visible model names.

**→ [Полное руководство по интеграции Amp CLI](https://help.router-for.me/agent-client/amp-cli.html)**

## Документация SDK

- Usage: [docs/sdk-usage.md](docs/sdk-usage.md)
- Advanced (executors & translators): [docs/sdk-advanced.md](docs/sdk-advanced.md)
- Access: [docs/sdk-access.md](docs/sdk-access.md)
- Watcher: [docs/sdk-watcher.md](docs/sdk-watcher.md)
- Пример кастомного провайдера: `examples/custom-provider`

## Лицензия

Проект распространяется по лицензии MIT. Подробности см. в файле [LICENSE](LICENSE).
