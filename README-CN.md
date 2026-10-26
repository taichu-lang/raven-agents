# raven-agents

<p align="center">
  <a href="README.md">English</a> | <b>简体中文</b>
</p>

基于 Go 的 LLM Agent 框架：由 harness 主循环驱动多轮对话、工具调用、Skill、人工审批
（human-in-the-loop）、事件流与持久化，并提供一个通过 HTTP/SSE 对外暴露的 API 服务。

配套 UI：[raven-ai](https://github.com/taichu-lang/raven-ai)——为当前 agent 适配的 UI。

## 架构设计

框架分为两层：

- **Harness Loop（外层）** —— `Agent` 负责控制流：turn 生命周期、工具激活、上下文构建、
  预算控制、人工审批暂停、checkpoint 与事件广播。
- **Middleware Chain（内层）** —— `llm.Runner` 只包裹一次 LLM 调用：洋葱式 middleware
  （结构化输出、日志等）嵌套在可插拔的 provider 适配器之外，输出类型化的流式响应块。

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
│    (当前实现 OpenAI Responses API；underlying.Provider       │
│     是扩展点)                                                │
├──────────────────────────────────────────────────────────────┤
│  EventBus (turn/iteration/llm/tool/runtime events)           │
│  Repository (Postgres / SQLite / discard) · Sandbox · Tracer │
└──────────────────────────────────────────────────────────────┘
```

### 核心组件

| 组件         | 包                                                                                     | 职责                                                                                                                                                                                                                                          |
| ------------ | -------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Agent 主循环 | [`agent`](agent/agent.go)                                                              | `Run` 开启一个 turn，`Resume` 续跑等待审批的 turn；循环执行 LLM 调用 → 工具调用，直到完成、暂停或超出预算。同一轮 iteration 内的多个 tool call 并发执行；每条消息产生时即 checkpoint 落库。                                                   |
| 状态         | [`agent/state.go`](agent/state.go)                                                     | 可序列化的 turn 级状态：状态机（`idle` / `running` / `waiting_approval` / `done` / `failed` / `interrupted`）、消息、待处理 tool call、已激活 skill、token 用量。新建的 Agent 会从 store 恢复历史 turn，任意进程都能续跑同一会话。            |
| LLM Runner   | [`agent/llm`](agent/llm/llm.go)                                                        | 编译 middleware 链，从 provider 流式输出 `delta` / `usage` / `final` 响应块（[`agent/llm/openai`](agent/llm/openai/responses.go) 实现 OpenAI Responses API；[`agent/llm/underlying`](agent/llm/underlying/provider.go) 定义 provider 抽象）。 |
| 工具         | [`agent/tool.go`](agent/tool.go)、[`tool/functool`](tool/functool)                     | `ToolRegistry` 通过 selector 函数按 turn 计算激活的工具集合。内置工具：`Skill`（按名加载 skill）、`exec-command`（配置 sandbox 时注册）、`question`（人工审批）。`functool` 把普通 Go 函数变成工具，自动派生 JSON schema 签名。               |
| Skill        | [`agent/skill.go`](agent/skill.go)、[`agent/skill`](agent/skill/loader.go)             | 从目录（如 `~/.agents/skills`）加载的可复用 `SKILL.md` 指令包。用户可以显式勾选（turn 开始前改写为 `Skill` tool call + tool result 消息对），模型也可以按名自主拉取——两条路径对模型呈现完全相同的形态。详见 [Skill 设计](docs/skills.md)。    |
| Hook         | [`agent/hook`](agent/hook/hook.go)、[`agent/hooks.go`](agent/hooks.go)                 | 命名扩展点（`user_input`、`pre_tool_use`、`post_tool_use`），返回 `allow` / `deny` / `ask` 决策；`ask` 会把 turn 置为 `waiting_approval` 并发出人工审批事件。                                                                                 |
| 事件         | [`internal/event`](internal/event/source.go)                                           | 异步广播总线（`turn.start/end`、`iteration.start/end`、`llm.start/delta/usage/complete/error`、`tool.call/result`、`runtime.hitl/error`）。API 服务通过 SSE 推给客户端；tracer 消费事件用于观测。                                             |
| 持久化       | [`agent/persistent`](agent/persistent/store.go)、[`migrations`](migrations)            | turn / message 存储，支持 Postgres、SQLite（bun）或无状态的 discard store。同一会话的 turn 拥有连续序号；消息只追加不修改。                                                                                                                   |
| Sandbox      | [`sandbox`](sandbox/sandbox.go)                                                        | `exec-command` 的隔离执行环境抽象；每个 agent 绑定一个 sandbox 实例，由可插拔的 `Provider` 创建（当前为本地进程，后续支持容器）。详见 [Sandbox 设计](docs/sandbox.md)。                                                                       |
| 可观测性     | [`internal/observability`](internal/observability)、[`agent/trace.go`](agent/trace.go) | 基于事件流接入 OpenTelemetry tracing；全局使用 `slog` 结构化日志。                                                                                                                                                                            |
| API 服务     | [`apiserver`](apiserver)、[`cmd/apiserver`](cmd/apiserver)                             | DDD 分层的聊天服务（依赖规则：`handler → application → domain ← infrastructure`），通过 HTTP + SSE 暴露多轮对话、skill 选择与人工审批续跑。                                                                                                   |

设计文档：[整体架构](docs/architecture.md) · [Skill 设计](docs/skills.md) ·
[Sandbox 设计](docs/sandbox.md)

## 示例

所有示例通过环境变量读取 provider 配置：

```sh
export OPENAI_APIKEY=<your-api-key>
export OPENAI_API=<responses-api-endpoint>   # 例如 https://api.openai.com/v1/responses
```

### 01 — 入门（LLM Runner）

直接使用 `llm.Runner` / middleware 链，不涉及 harness 主循环。

| 示例                                                                         | 说明                                                          |
| ---------------------------------------------------------------------------- | ------------------------------------------------------------- |
| [01_hello_agent](examples/01-get-started/01_hello_agent/main.go)             | 最小化单次调用：构建 runner，发送一条用户消息，消费流式响应。 |
| [02_multi_turn](examples/01-get-started/02_multi_turn/main.go)               | 自行维护消息列表，在 runner 层完成多轮对话。                  |
| [03_analyze_image](examples/01-get-started/03_analyze_image/main.go)         | 多模态输入：在消息中附带图片文件，让模型分析。                |
| [04_structured_output](examples/01-get-started/04_structured_output/main.go) | 通过结构化输出 middleware 把响应约束到 Go struct。            |

### 02 — Harness（Agent 主循环）

由事件总线驱动的完整 `Agent` 实例。

| 示例                                                               | 说明                                                                            |
| ------------------------------------------------------------------ | ------------------------------------------------------------------------------- |
| [01_hello_harness](examples/02-harness/01_hello_harness/main.go)   | 最小化 agent：订阅事件总线，跑一个 turn，观察 turn / iteration / LLM 事件流。   |
| [02_function](examples/02-harness/02_function/main.go)             | 用 `functool` 注册自定义函数工具，让模型串联调用（天气 → 旅行规划）。           |
| [03_multi_turn](examples/02-harness/03_multi_turn/main.go)         | 同一会话多轮对话：每次 `Run` 追加一个 turn 并恢复历史。                         |
| [04_postgres_store](examples/02-harness/04_postgres_store/main.go) | 把 turn 和消息持久化到 Postgres，会话跨 agent 实例存续。                        |
| [05_hello_skill](examples/02-harness/05_hello_skill/main.go)       | 在 `$HOME/.agents/skills` 安装一个 `SKILL.md`，让 agent 通过 `Skill` 工具加载。 |

运行任意示例：

```sh
go run ./examples/01-get-started/01_hello_agent
```

## 目录结构

```
agent/          harness 主循环、状态、工具、skill、hook、持久化
  llm/          middleware 链 + provider 适配器（openai、underlying）
  persistent/   turn / message 存储
apiserver/      DDD 分层的 HTTP/SSE 聊天服务（handler/application/domain/infrastructure）
cmd/apiserver/  API 服务入口
internal/       database、事件总线、可观测性
sandbox/        执行环境抽象（+ 本地 provider）
tool/           工具接口与 functool 辅助包
migrations/     Postgres 与 SQLite schema 迁移
docs/           设计文档
examples/       可运行示例（入门、harness）
```

## 许可证

采用 [PolyForm Noncommercial License 1.0.0](LICENSE) 许可——非商业用途可自由使用和修改，
不允许商业用途。
