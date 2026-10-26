# raven-agents

<p align="center">
  <b>English</b> | <a href="README-CN.md">简体中文</a>
</p>

A Go framework for building LLM agents: a harness loop that drives multi-turn conversations,
tool calling, skills, human-in-the-loop approval, event streaming and persistence — plus an
API server that exposes it all over HTTP/SSE.

Companion UI: [raven-ai](https://github.com/taichu-lang/raven-ai) — a UI adapted for this agent.

## Architecture

The framework is split into two layers:

- **Harness Loop (outer layer)** — the `Agent` owns control flow: turn lifecycle, tool
  activation, context building, budget, human-in-the-loop pausing, checkpointing and events.
- **Middleware Chain (inner layer)** — the `llm.Runner` wraps a single LLM call only:
  onion-style middlewares (structured output, logging, ...) around a pluggable provider
  adapter, yielding a stream of typed response chunks.

```
┌──────────────────────────────────────────────────────────────┐
│  apiserver (handler → application → domain ← infrastructure) │
│                 HTTP / SSE chat API                          │
└───────────────────────────┬──────────────────────────────────┘
                            │
┌───────────────────────────┴──────────────────────────────────┐
│  Agent — Harness Loop                                        │
│    Run / Resume → turn → iterations → tool calls             │
│    AgentState · Budget · Hook Registry (user_input,          │
│    pre_tool_use, post_tool_use) · ToolRegistry · SkillRegistry│
├──────────────────────────────────────────────────────────────┤
│  llm.Runner — Middleware Chain                               │
│    structured_output → logger → provider adapter             │
│    (OpenAI Responses API today; underlying.Provider is the   │
│    extension point)                                          │
├──────────────────────────────────────────────────────────────┤
│  EventBus (turn/iteration/llm/tool/runtime events)           │
│  Repository (Postgres / SQLite / discard) · Sandbox · Tracer │
└──────────────────────────────────────────────────────────────┘
```

### Core components

| Component     | Package                                                                                | Responsibility                                                                                                                                                                                                                                                                                               |
| ------------- | -------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Agent loop    | [`agent`](agent/agent.go)                                                              | `Run` starts a turn, `Resume` continues one paused for approval; iterates LLM call → tool execution until done, paused, or the budget is exceeded. Tool calls within an iteration run concurrently; every message is checkpointed to the store as it is produced.                                            |
| State         | [`agent/state.go`](agent/state.go)                                                     | Serializable per-turn state: status (`idle` / `running` / `waiting_approval` / `done` / `failed` / `interrupted`), messages, pending tool calls, activated skills, token usage. A fresh Agent restores prior turns from the store, so any process can resume a conversation.                                 |
| LLM runner    | [`agent/llm`](agent/llm/llm.go)                                                        | Compiles the middleware chain and streams `delta` / `usage` / `final` chunks from the provider ([`agent/llm/openai`](agent/llm/openai/responses.go) implements the OpenAI Responses API; [`agent/llm/underlying`](agent/llm/underlying/provider.go) defines the provider abstraction).                       |
| Tools         | [`agent/tool.go`](agent/tool.go), [`tool/functool`](tool/functool)                     | `ToolRegistry` computes the active tool set per turn via selector functions. Builtin tools: `Skill` (load a skill by name), `exec-command` (registered when a sandbox is configured), `question` (human-in-the-loop). `functool` turns a plain Go function into a tool with a JSON-schema-derived signature. |
| Skills        | [`agent/skill.go`](agent/skill.go), [`agent/skill`](agent/skill/loader.go)             | Reusable `SKILL.md` instruction packs loaded from directories (e.g. `~/.agents/skills`). Users can select skills eagerly (rewritten into a `Skill` tool-call/result pair before the turn) and the model can pull them lazily by name — both paths look identical to the model.                               |
| Hooks         | [`agent/hook`](agent/hook/hook.go), [`agent/hooks.go`](agent/hooks.go)                 | Named extension points (`user_input`, `pre_tool_use`, `post_tool_use`) returning `allow` / `deny` / `ask` decisions; `ask` pauses the turn into `waiting_approval` and emits a HITL event.                                                                                                                   |
| Events        | [`internal/event`](internal/event/source.go)                                           | Async broadcast bus (`turn.start/end`, `iteration.start/end`, `llm.start/delta/usage/complete/error`, `tool.call/result`, `runtime.hitl/error`). The API server streams them to clients over SSE; the tracer consumes them for observability.                                                                |
| Persistence   | [`agent/persistent`](agent/persistent/store.go), [`migrations`](migrations)            | Turn/message store backed by Postgres or SQLite (bun), or a discard store for stateless use. Turns get contiguous sequence numbers per conversation; messages are append-only.                                                                                                                               |
| Sandbox       | [`sandbox`](sandbox/sandbox.go)                                                        | Isolated execution environment abstraction for `exec-command`; one sandbox instance per agent, provisioned by a pluggable `Provider` (local process today, containers later).                                                                                                                                |
| Observability | [`internal/observability`](internal/observability), [`agent/trace.go`](agent/trace.go) | OpenTelemetry tracing wired from the event stream; structured `slog` logging throughout.                                                                                                                                                                                                                     |
| API server    | [`apiserver`](apiserver), [`cmd/apiserver`](cmd/apiserver)                             | DDD-layered chat service (dependency rule: `handler → application → domain ← infrastructure`) exposing multi-turn conversations, skill selection and HITL resume over HTTP with SSE streaming.                                                                                                               |

## Examples

All examples read the provider configuration from environment variables:

```sh
export OPENAI_APIKEY=<your-api-key>
export OPENAI_API=<responses-api-endpoint>   # e.g. https://api.openai.com/v1/responses
```

### 01 — Get started (LLM runner)

Direct use of the `llm.Runner` / middleware chain, without the harness loop.

| Example                                                                      | Description                                                                                     |
| ---------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| [01_hello_agent](examples/01-get-started/01_hello_agent/main.go)             | Minimal single-shot call: build a runner, send one user message, consume the streamed response. |
| [02_multi_turn](examples/01-get-started/02_multi_turn/main.go)               | Keep the message list yourself and carry on a multi-turn conversation at the runner level.      |
| [03_analyze_image](examples/01-get-started/03_analyze_image/main.go)         | Multimodal input: attach an image file to a message and have the model analyze it.              |
| [04_structured_output](examples/01-get-started/04_structured_output/main.go) | Constrain the response to a Go struct via the structured-output middleware.                     |

### 02 — Harness (agent loop)

Full `Agent` instances driven by the event bus.

| Example                                                            | Description                                                                                              |
| ------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------- |
| [01_hello_harness](examples/02-harness/01_hello_harness/main.go)   | Minimal agent: subscribe to the event bus, run one turn, watch the turn/iteration/LLM events flow by.    |
| [02_function](examples/02-harness/02_function/main.go)             | Register custom function tools with `functool` and let the model chain them (weather → travel planning). |
| [03_multi_turn](examples/02-harness/03_multi_turn/main.go)         | Multiple turns on one conversation: each `Run` appends a turn and restores the history.                  |
| [04_postgres_store](examples/02-harness/04_postgres_store/main.go) | Persist turns and messages to Postgres so the conversation survives across agent instances.              |
| [05_hello_skill](examples/02-harness/05_hello_skill/main.go)       | Install a `SKILL.md` under `$HOME/.agents/skills` and let the agent load it through the `Skill` tool.    |

Run any example with:

```sh
go run ./examples/01-get-started/01_hello_agent
```

## Project layout

```
agent/          Harness loop, state, tools, skills, hooks, persistence
  llm/          Middleware chain + provider adapters (openai, underlying)
  persistent/   Turn/message store
apiserver/      DDD-layered HTTP/SSE chat service (handler/application/domain/infrastructure)
cmd/apiserver/  API server entrypoint
internal/       database, event bus, observability
sandbox/        Execution environment abstraction (+ local provider)
tool/           Tool interface and functool helper
migrations/     Postgres and SQLite schema migrations
docs/           Design documents (中文)
examples/       Runnable examples (get-started, harness)
```

## License

Licensed under the [PolyForm Noncommercial License 1.0.0](LICENSE) — free to use and
modify for non-commercial purposes; commercial use is not permitted.
