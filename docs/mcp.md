# MCP 集成设计

> 本文描述 MCP（Model Context Protocol）server 集成的设计：目标形态、关键设计问题的讨论结论、接口定义与本期落地的最小集。总体架构见 [architecture.md](architecture.md)，延迟发现的厂商语义参考见 [tool_search.md](tool_search.md)。

## 一、目标

MCP 让 agent 使用外部 server 提供的工具。集成要覆盖三层能力：

1. **工具桥接**：MCP server 的工具适配为框架的 `tool.FuncTool`，经现有 `ToolRegistry` / dispatch 路径供任意大模型调用——provider 适配器不感知 MCP 的存在。
2. **延迟发现**：MCP server 一多，工具全集全量广播进 tools 参数会 token 膨胀、选择质量下降。MCP 工具不进广播集，模型经 tool-search 元工具按需发现，后续调用经 registry dispatch。同一套语义按 provider 能力分两条执行路径：**降级路径（普通 function 工具）是通用实现**，任何 OpenAI 兼容端点可用；原生 `tool_search`（OpenAI client 执行）是能力位开启的优化（见 3.5）。
3. **安装/启用管理**：管理员维护可信 server 目录（catalog，含命令、版本、所需密钥声明），用户从目录安装并只填写密钥，启用/禁用决定哪些 server 的工具进入 registry。用户自定义 server 仅开放 http 传输；stdio server 只能来自 catalog 或静态 `$AppDir/.agents/mcp.json`（全局，启动时加载）——stdio 即在宿主机拉起子进程，不能由用户任意提交命令。

MCP client 采用官方 Go SDK（`github.com/modelcontextprotocol/go-sdk`）。

## 二、厂商语义对比（延迟发现）

| | OpenAI Responses API | Claude Messages API |
| - | -------------------- | ------------------- |
| 搜索工具 | `{type: "tool_search", execution: "client"\|"server"}`；client 模式由客户端声明 `description` 与 `parameters`（检索参数 schema 自定） | `{type: "tool_search_20250919"}` 服务端工具 |
| 延迟标记 | 工具带 `defer_loading: true` | 工具带 `defer_loading: true` |
| 搜索执行 | client 模式：**客户端执行检索**；server 模式：API 侧检索 | 仅服务端执行，API 侧检索 |
| 模型的搜索调用 | `tool_search_call` 特殊 item（非 function_call），`arguments` 为 JSON 对象（形状由声明的 parameters 决定） | tool_use（name=tool_search，服务端内联执行） |
| 搜索结果 | `tool_search_output` 特殊 item（非 function_call_output），带 `call_id` / `execution` / `status`，**携带命中工具的完整定义**；另有 `additional_tools`（role: developer）注入项 | `tool_search_result` 特殊内容块（API 生成，仅紧随 tool_search tool use 有效） |
| 命中工具可用性 | 由 tool_search_output 携带的定义加载，随会话历史生效 | API 自动注入 |
| 分组 | `namespace` 工具类型，`function_call` 带 `namespace` 字段 | 无 |

字段以 openai-go v3.56.0 的 `responses.ResponseToolSearchCall` / `ResponseToolSearchOutputItem` / `ToolSearchToolParam` 为准。

关键差异：**OpenAI 支持 client 执行**（框架自己检索，结果以特殊 item 返回）；Claude 原生仅 server 执行。Codex 用 client 执行 + BM25。

**能力探测**：默认部署是 `qwen3.8-max` + OpenAI 兼容端点（`RAVEN_API`），兼容网关大概率不支持 `tool_search` 这类私有 item。因此原生语义不能作为唯一路径：provider 以 `ProviderOptions.NativeToolSearch` 声明能力，agent 据此选择原生路径或降级路径（3.5）；接入新端点前先以带 `{type: "tool_search", execution: "client"}` 的请求探测。

框架的统一语义模型（provider 无关）：

```
tools 参数:  [skill, exec-command, question, tool-search(, mcp-call)]   # MCP 工具不出现
模型输出:    tool_search_call {arguments}                              # 原生:特殊内容;降级:普通 function_call
框架执行:    ToolRegistry.Search(query, limit) → 命中工具定义           # 注入的 ToolSearcher,默认 BM25
返回模型:    tool_search_output {tools: [...定义...]}                  # 原生:特殊消息;降级:普通 tool result
后续调用:    原生: 模型调 mcp__<server>__<tool> → registry dispatch
            降级: 模型调 mcp-call {name, arguments} → registry dispatch
```

## 三、关键设计决策

### 3.1 模型无关性：桥接只落在 tool.FuncTool 抽象上

MCP 工具适配为 `tool.FuncTool`：`Name()` / `Description()` / `InSchema()` / `Call()` 转发到 server 会话。schema 从 server 原样取（wire 上是 `map[string]any`，重新 marshal → unmarshal 为 `*jsonschema.Schema`），不用 `functool.New`（它靠反射派生 schema）。

工具名 `mcp__<server>__<tool>`（Claude Code 约定）：

- server 名在 Validate 时限制为 `[a-z0-9-]`、禁止含 `__`，不靠事后清洗——保证名字可逆，PreToolUse 按 `mcp__<server>__` 前缀匹配无歧义；
- tool 名中 `[A-Za-z0-9_-]` 之外清洗为 `_`，全名超 64 字节截断 + 8 位 FNV-1a 摘要保唯一（OpenAI Responses 函数名约束）；dispatch 时转发 server 侧原名；
- 不使用 OpenAI `namespace`（server 即 namespace 虽是原生建模）：扁平名跨 provider 通用，`ToolCallContent` 无需增加 namespace 字段。

### 3.2 延迟发现：注册与广播分离，检索入口在 ToolRegistry、算法注入

registry 新增 `RegisterDeferred`：注册进 dispatch 全集、永不进入广播集——`Active()` 跳过，`Get()` 可查。这是 MCP 工具进入 registry 的唯一方式：模型经搜索发现后按名调用时能 dispatch，但 tools 参数里永远没有它们。

**registry 提供 `Search` 入口，检索算法由构造时注入的 `ToolSearcher` 实现**（nil 时默认 BM25）。registry 只负责转发，不含检索算法；检索范围的约束则由 registry 自己保证：

- **候选集即 deferred 集合**：`Search` 在读锁内取 deferred 集合快照、释放锁后把快照作为 `candidates` 传给 searcher——外部调用（如 embedding）不持锁；
- **结果兜底过滤**：searcher 返回的结果再与 deferred 集合求交，集合外的命中丢弃——即便某个实现（如外部向量索引）未按候选集过滤，"能搜到"与"能 dispatch"也恒为同一集合；
- **天然按请求隔离**：registry 每请求新建（`apiserver/application/chat.go:87`），只含本用户启用集的工具，无需全局索引，也无需调用方另行装配 Searcher；
- 是否广播 tool-search 由 registry 是否有 deferred 工具决定，`AgentOptions` 不增加检索相关字段。

| 关注点 | 归属 |
| ------ | ---- |
| dispatch（名字 → FuncTool） | `ToolRegistry`（`RegisterDeferred` / `Get`） |
| 检索入口与范围约束（query → deferred 工具） | `ToolRegistry.Search`（候选集 = deferred 集合，结果兜底过滤） |
| 检索算法（候选排序） | 注入的 `ToolSearcher`（默认 BM25） |
| 广播（tools 参数） | provider 适配器（内置工具 + tool-search，降级路径另加 mcp-call） |

### 3.3 检索：内存 BM25

单用户启用的 MCP 工具通常为几十到几百个，内存 BM25 足够，且无外部依赖：

- **语料**：每个工具的可检索文本 = server 名 + 工具名 + description + 输入参数名；分词按 `_` / `-` / camelCase / 空白切分并小写；
- **构建**：首次 `Search` 时以候选集惰性建索引，`RegisterDeferred` 新增工具时标记失效、下次检索重建；registry 每请求新建，实际每请求至多建一次（百级文档，成本可忽略），不持久化索引，无一致性问题；
- **存储无关**：Postgres 与 sqlite 部署共用同一实现；
- 向量检索（pgvector）作为扩展路径（八-4），构造 registry 时注入另一个 `ToolSearcher` 即可，须按 `candidates` 限定命中。

### 3.4 特殊消息：搜索调用与结果是一等消息内容

`underlying` 消息层新增两个内容类型，随会话持久化，provider 适配器映射为各自的原生形式：

- `ToolSearchCallContent{ID, CallID, Arguments json.RawMessage}` — 模型的搜索调用，**位于 assistant 消息**。`Arguments` 原样保存，由 agent 按自己声明的 tool-search parameters 解析 query / limit——换 schema 不动消息层与持久化格式。
- `ToolSearchResultContent{CallID, Tools []*ToolDefinition}` — 搜索结果，**位于 RoleTool 消息**（与 `ToolResultContent` 并列），携带命中工具的完整定义（name + description + input schema）。

现有代码必须同步修改的位置（否则 panic 或会话无法恢复）：

| 位置 | 现状 | 修改 |
| ---- | ---- | ---- |
| `agent/llm/openai/message.go:246` `inputContentFromAssistant` | 未知 content panic | `ToolSearchCallContent` → `tool_search_call` item |
| `agent/llm/openai/tool.go:71` `inputItemsFromTool` | 非 `ToolResultContent` panic | `ToolSearchResultContent` → `tool_search_output` item（`execution: client`、`status: completed`） |
| `agent/llm/underlying/content.go:310` `unmarshalMessageContent` | 未知 type 报错 → `restoreIfPresent` 失败 → `agent.New` panic | 新增 `ContentKind` 常量 + `MarshalJSON` + unmarshal 分支 |
| `agent/llm/openai/event.go:75`、`responses.go:110` | 不解析 `tool_search_call` | 流式与非流式均解析 `ResponseToolSearchCall` |

**会话历史须可被任意适配器渲染**：`req.Model` 每请求可换，同一会话可能先后经原生与非原生 provider。不支持原生能力的适配器把 `ToolSearchCallContent` 降级渲染为 `function_call(tool-search)`、`ToolSearchResultContent` 降级渲染为 `function_call_output`（定义序列化为文本），保证混合历史不中断。

命中工具的可用性由 provider 依据特殊消息管理（定义随消息进入会话历史），**框架不维护 Discovered/exposure 状态**：广播集每 turn 恒定，无需每 iteration 重算，prompt cache 友好。

### 3.5 两条执行路径

| | 原生路径（`NativeToolSearch=true`） | 降级路径（默认） |
| - | ---------------------------------- | ---------------- |
| 广播 | 内置工具 + `{type: tool_search, execution: client, description, parameters}` | 内置工具 + `tool-search` function + `mcp-call` function |
| 搜索调用 | `tool_search_call` → `ToolSearchCallContent` | `function_call(tool-search)` → `ToolCallContent` |
| 搜索结果 | `ToolSearchResultContent` → `tool_search_output` | `ToolResultContent`，文本含命中工具的 name / description / input schema |
| 命中工具调用 | 模型直接调 `mcp__<server>__<tool>` | 模型调 `mcp-call{name, arguments}`，按 name 查 registry 并以工具 `InSchema()` 校验 arguments 后 dispatch |
| tools 参数 | 回合内恒定 | 回合内恒定（不随发现集变化，不破坏 prompt cache） |

降级路径的代价是 provider 端不再按工具 schema 约束参数生成，由 `mcp-call` 在 dispatch 时校验并以 IsError 结果让模型自纠。两条路径共用 registry（dispatch 与 `Search`），只在 agent 循环的分发与适配器映射上不同。

### 3.6 agent 循环修正

- **终止条件**：`agent/agent.go:284` 现以 `len(State.ToolCalls) == 0` 判定回合结束，模型只输出 `tool_search_call` 时会被误判为 Done。改为 `ToolCalls` 与 `ToolSearches` 均为空才结束；`resetPerTurn` 同时清空 `ToolSearches`。
- **执行时机**：搜索在 `beforeToolCall` **之前**执行并 checkpoint 结果。若同一响应还含 `question` 或 hook `ask`，回合会在 `invokeToolCalls` 中暂停；先落库搜索结果，resume 时就不存在无 output 的 `tool_search_call`，`ToolSearches` 也无需进入持久化的 turn 状态。
- **可发现性**：tool-search 的 description 列出本次启用的 server（名字 + 一句话描述），否则模型不知道有能力可搜；该列表回合内恒定，不影响 cache。registry 无 deferred 工具时不广播 tool-search / mcp-call。
- 搜索与工具调用在同一 iteration 内执行，不额外消耗 iteration 预算。

### 3.7 会话生命周期：进程级 Manager

每个 HTTP 请求都新建 agent（registry per-request），而 stdio MCP server 是子进程、http server 是长连接——**会话必须进程级复用**。`mcp.Manager` 持全部会话，agent 只见到 `tool.FuncTool` 快照。

- server 以不透明 `key` 标识（`<userID>/<name>`；静态配置 `static/<name>`），框架不感知用户概念；
- **工具清单快照持久化**：连接成功或 `ToolListChanged` 时把工具清单写入 DB（`mcp_installations.tools` JSON 列）；agent 构建、注册、检索都读快照，**不依赖实时连接**——chat 请求不会被 N 个 server 的连接超时阻塞，进程重启后无需预连接所有用户的 server；
- **懒连接**：首次工具调用时连接；重连以 singleflight 按 key 合并，避免并发调用同时重连；
- server 断连（`session.Wait()` 返回）标记 down；下次工具调用重连一次重试；`jsonrpc.Error`（协议错误）不触发重连；
- stdio 会话空闲超时回收（默认 10 分钟）；单次 `CallTool` 带超时，请求 ctx 取消随 MCP cancellation 通知传给 server；
- 单个 server 连接失败 fail-soft（warn 跳过）。

### 3.8 安装/启用：catalog + 安装记录 + 启用集过滤

- `mcp_catalog`：管理员维护的可信 server 模板（名称、描述、传输、命令 / URL 模板、版本、所需密钥声明），stdio server 只能来源于此或静态 mcp.json；
- `mcp_installations`：用户安装记录（UNIQUE(user_id, name)），引用 `catalog_id` 或为用户自定义 http server；含用户密钥、`enabled`、工具清单快照；
- **密钥**：Env / Headers 中的密钥以 `golang.org/x/crypto` 加密落库，API 响应脱敏（仅返回键名），`slog` 不输出 Env / Headers；
- 安装：Validate → 入库（enabled）→ 尝试连接并写入工具快照；连接失败不回滚安装（状态可见可重试）；
- 卸载：删库 + `Manager.Disconnect`；禁用：翻转标志 + `Manager.Disconnect`（不让子进程常驻）；启用：翻转标志，下次调用懒连接——**启用集只在 agent 构建时过滤进 registry**；
- 时间字段（`created_at` / `updated_at`）用秒级时间戳数字，由应用层赋值；
- 静态 `$AppDir/.agents/mcp.json` 的 server 视为全局（所有用户、始终启用）。

### 3.9 权限：MCP 工具默认门控

`hook.Registry` 无注册 hook 时直接返回 Allow（`agent/hook/registry.go:47`），而 MCP 工具是第三方实现、可能有副作用，需要默认策略：

- 工具 annotations 中 `readOnlyHint=true` → allow；
- 其余 MCP 工具 → ask，复用现有 `waiting_approval` / resume 流程；
- 以 PreToolUse hook 实现，按 `mcp__<server>__` 前缀识别；降级路径按 `mcp-call` 的 `name` 参数识别；用户可按 server 设置信任（全部 allow）。

## 四、目标设计（接口）

框架包 `mcp/`（与 `sandbox/` 平级；SDK 别名 `mcpsdk`）：

```go
type ServerConfig struct {
    Name      string            // 用户可见名,[a-z0-9-],不含 "__"
    Transport string            // "stdio" | "http";mcp.json 隐式判别:有 command → stdio,有 url → http
    Command   string            // stdio
    Args      []string
    Env       map[string]string
    URL       string            // http
    Headers   map[string]string
    Timeout   time.Duration     // 单次连接超时,默认 30s
}

// LoadStaticConfig 读取 $AppDir/.agents/mcp.json(Claude Code mcpServers 约定);
// 文件缺失返回 (nil, nil);env 变量逐字段展开
func LoadStaticConfig(path string) ([]ServerConfig, error)

// ToolSnapshot 是持久化的工具清单快照,agent 构建与检索只读快照。
type ToolSnapshot struct {
    Name        string          // server 侧原名
    Description string
    InputSchema json.RawMessage
    ReadOnly    bool            // annotations.readOnlyHint
}

type Manager struct { /* ... */ }
func NewManager(logger *slog.Logger) *Manager
func (m *Manager) Connect(ctx context.Context, key string, cfg ServerConfig) ([]ToolSnapshot, error)
func (m *Manager) Disconnect(key string) error
// Tools 以快照构建 FuncTool,调用时懒连接
func (m *Manager) Tools(key string, cfg ServerConfig, snapshot []ToolSnapshot) []tool.FuncTool
// OnToolListChanged 注册快照回写回调
func (m *Manager) OnToolListChanged(fn func(key string, tools []ToolSnapshot))
func (m *Manager) Status() []ServerStatus // {Name, Connected, Tools, LastError}
func (m *Manager) Close(ctx context.Context) error                            // 幂等
```

catalog 条目 `ServerTemplate` 在 apiserver domain 层定义：`ServerConfig` 的模板形式，Env / Headers 中以 `${SECRET_NAME}` 引用用户填写的密钥，安装时渲染为 `ServerConfig`。

工具桥接（`mcp/tool.go`）：手写 `mcpTool` 实现 `tool.FuncTool`（`state *serverState` / `rawName` / `name` / schema），`Call` 转发 `session.CallTool`：

- 结果映射：`StructuredContent` 优先，否则拼接文本；`IsError=true` → error（`invokeOneTool` 渲染 IsError，模型自纠）；
- 结果超过上限（默认 64 KiB）截断并追加 `[output truncated]`，与 exec-command 同语义；
- `invokeOneTool`（`agent/agent.go:472`）会对返回值再 `json.Marshal`，string 结果会被编码为带引号的 JSON 字符串；文本结果以 `json.RawMessage` 或结构化对象返回，避免二次转义。

消息层（`agent/llm/underlying`）：

```go
// ToolDefinition 是延迟发现流转中的可序列化工具定义(name + description + input schema)。
type ToolDefinition struct {
    Name        string             `json:"name"`
    Description string             `json:"description,omitzero"`
    InputSchema *jsonschema.Schema `json:"input_schema,omitzero"`
}

// ToolSearchCallContent 是模型发起的搜索调用,位于 assistant 消息
// (OpenAI: tool_search_call;非普通 ToolCallContent)。
type ToolSearchCallContent struct {
    ID        string          `json:"id,omitzero"`
    CallID    string          `json:"call_id"`
    Arguments json.RawMessage `json:"arguments"`
}

// ToolSearchResultContent 是搜索结果,位于 RoleTool 消息,携带命中工具定义
// (OpenAI: tool_search_output;非 ToolResultContent)。
type ToolSearchResultContent struct {
    CallID string            `json:"call_id"`
    Tools  []*ToolDefinition `json:"tools"`
}
```

provider 能力位（`agent/llm/underlying`）：

```go
type ProviderOptions struct {
    // ...
    // NativeToolSearch reports whether the endpoint supports the client-executed tool_search item;
    // false selects the function-tool fallback.
    NativeToolSearch bool
}
```

检索接口（框架声明并提供默认 BM25 实现）：

```go
// ToolSearcher ranks the candidate tools for a query. The registry passes its deferred set as
// candidates, so an implementation backed by an external index must restrict its hits to them.
type ToolSearcher interface {
    Search(ctx context.Context, query string, limit int, candidates []tool.FuncTool) ([]tool.FuncTool, error)
}

// NewBM25Searcher returns the default in-memory searcher, which indexes the candidates lazily.
func NewBM25Searcher() ToolSearcher
```

registry（`agent/tool.go`）：

```go
// NewToolRegistry creates a registry; a nil searcher falls back to NewBM25Searcher.
func NewToolRegistry(searcher ToolSearcher) *ToolRegistry

// RegisterDeferred registers a dispatch-only tool: it is never advertised in the tools
// param; it enters the model's view only via the search result message. Get() covers it.
func (r *ToolRegistry) RegisterDeferred(t tool.FuncTool)

// HasDeferred reports whether any dispatch-only tool is registered, which decides whether the
// tool-search tool is advertised.
func (r *ToolRegistry) HasDeferred() bool

// Search resolves deferred tools for the query. It snapshots the deferred set under the read
// lock, calls the searcher without holding it, and drops hits outside the deferred set.
func (r *ToolRegistry) Search(ctx context.Context, query string, limit int) ([]tool.FuncTool, error)
```

registry 返回 `tool.FuncTool`，由 agent 转换为 `underlying.ToolDefinition` 放进 `ToolSearchResultContent`——检索接口不依赖消息层类型。

内置工具（`agent/tool.go`，registry 有 deferred 工具时注册）：

- `tool-search`：降级路径的 function 工具，参数 `{query, limit}`，description 列出启用的 server；原生路径下同一 schema 作为 `tool_search` 的 `parameters`；
- `mcp-call`：降级路径专用，参数 `{name, arguments}`，按 name 查 deferred 工具、以 `InSchema()` 校验后 dispatch。

agent 循环（registry 有 deferred 工具时生效）：

- 广播集：内置工具 + tool-search（原生 / function 形式由能力位决定）+ 降级路径的 mcp-call；
- 流式 final chunk 中的 `ToolSearchCallContent` 收集进 `State.ToolSearches`；
- iterate：先执行挂起的搜索（`ToolRegistry.Search`）→ `ToolSearchResultContent` 消息 → checkpoint，再进入 `beforeToolCall` / 工具调用；
- 终止条件：`ToolCalls` 与 `ToolSearches` 均为空；
- 搜索执行发 `tool.search` 事件（query + 命中工具名），SSE 转发，UI 可感知。

管理层（DDD）：

- `mcp_catalog`、`mcp_installations` 表 + 对应 store（模式同 skill/user），列表函数返回空切片而非 nil；
- `MCPService`：Catalog 列表 / List（合并 manager Status）/ Install / Uninstall / Enable / Disable；
- 路由：`GET /api/v1/mcp/catalog`、`GET/POST /api/v1/mcp/servers`、`DELETE /api/v1/mcp/servers/:name`、`POST /api/v1/mcp/servers/:name/enable|disable`；
- 用户身份：沿用现状固定 userID（表结构按多用户预留，auth 中间件接线为扩展项）。

## 五、感知

| 维度 | 机制 |
| ---- | ---- |
| 模型 | tools 参数只含内置 + tool-search（降级路径另含 mcp-call）；搜索结果携带定义；调用按名 dispatch |
| 客户端/UI | `tool.search`（query + 命中列表）、`tool.call` / `tool.result`（MCP 工具,名字前缀标识来源）事件,SSE 转发 |
| 权限 | 默认策略（3.9）：`readOnlyHint` 放行，其余 ask；PreToolUse hook 按 `mcp__<server>__` 前缀或 `mcp-call.name` 识别 |
| 运维 | `slog` 带 `provider: "mcp"`，不输出密钥；`Manager.Status()` 预留调试端点 |
| 跨回合 | 搜索结果留在历史中；server 禁用 / 卸载或 schema 变更后，模型按旧定义调用得到 not found 或参数错误并自纠 |

MCP server mid-turn 死亡时 `CallTool` 返回错误 → `IsError=true` 工具结果，模型自纠错（与 exec-command 非零退出码同语义）。

## 六、错误处理与生命周期

| 故障 | 行为 | 呈现 |
| ---- | ---- | ---- |
| 配置非法（安装请求） | Validate 失败,拒绝安装 | API 错误响应 |
| 安装时连接失败 | 保留安装记录,无快照则不进 registry | API 响应中的状态、Status |
| server 会话中断 | 标记 down;下次调用重连一次重试一次 | IsError 工具结果、日志 |
| 协议错误（未知工具等） | 原样返回,不重连 | IsError 工具结果 |
| `IsError=true` CallToolResult | 渲染为错误文本 | IsError 工具结果,模型自纠 |
| 工具 schema 不可转换 | 跳过该工具,保住 server | warn 日志 |
| 历史定义指向已禁用 / 卸载的工具 | registry 查无此名 | `tool not found` 工具结果,模型自纠 |
| `mcp-call` 参数不符合 schema | 不 dispatch | IsError 工具结果（含校验错误）,模型自纠 |
| 结果超长 | 截断至上限 | 工具结果末尾 `[output truncated]` |
| 检索无命中 | 返回空工具列表 | `tool.search` 事件、空搜索结果 |
| 非文本内容（image 等） | `[N image content omitted]` 占位 | 工具结果文本（已知限制） |

会话归属：进程（main 创建、退出关闭，stdio 子进程由 `CommandTransport.Close` SIGTERM 终止，空闲超时回收）；工具快照归属：DB（随连接 / `ToolListChanged` 回写），registry 中的 FuncTool 归属单个 chat 请求。

## 七、本期范围

| 项 | 说明 |
| -- | ---- |
| `mcp/` 框架包 | Manager（懒连接、singleflight 重连、断连监测、空闲回收）、工具桥接（截断）、工具快照、静态配置、传输 |
| registry + 检索 | `RegisterDeferred` / `HasDeferred` / `Search`（候选集约束 + 结果兜底过滤）、注入式 `ToolSearcher` 与默认 BM25 实现 |
| 执行路径 | 降级路径（`tool-search` + `mcp-call`）为通用实现；`NativeToolSearch` 能力位开启 OpenAI 原生 client tool_search |
| 消息层 + 适配器 | `ToolSearchCallContent` / `ToolSearchResultContent` / `ToolDefinition`，3.4 表中四处修改，非原生适配器的降级渲染 |
| agent 循环 | 终止条件、搜索先于 `beforeToolCall` 执行并 checkpoint、`tool.search` 事件 |
| 管理层 | `mcp_catalog` + `mcp_installations` 迁移、密钥加密与脱敏、快照回写、MCPService、HTTP API |
| 权限 | MCP 工具默认策略（`readOnlyHint` 放行，其余 ask） |
| 装配 | main 启动加载静态 server；ChatService 按启用集以快照 `RegisterDeferred` |

**刻意排除**：MCP resources / prompts、elicitation、sampling（协议 2026-07-28 已废弃，待核实出处）、roots、per-conversation 启用粒度、Claude 适配器（原生 server 执行映射）、向量检索、用户自定义 stdio server。

## 八、扩展路径

1. **Claude 适配器**：原生 server 执行——MCP 工具带 `defer_loading` 随请求发送，API 侧检索，`tool_search_result` 块保留于会话历史；框架检索在该 provider 上被旁路。
2. **elicitation**：SDK v1.8.0（待核实出处）原生 `ResultTypeInputRequired` 模式——`CallToolResult` 返回 `InputRequests` + `RequestState`，渲染为 HITL payload 走 `State.Pending` / `waiting_approval`，resume 变为"带 `InputResponses` 重入调用"（architecture.md Backlog 构想）。
3. **MCP resources**：`list-mcp-resources` / `read-mcp-resource` 内置工具。
4. **向量检索**：pgvector（`tool_embeddings` 表 + OpenAI 兼容 `/v1/embeddings`）或混合检索（向量 + BM25），构造 registry 时注入另一个 `ToolSearcher` 实现（按 `candidates` 限定命中）；需处理 embedding 维度随模型变化、索引与工具快照一致性、sqlite 部署回退。
5. **stdio 进入 sandbox**：stdio server 子进程由 sandbox provider 拉起，与宿主机隔离，之后可放开用户自定义 stdio。
6. **上下文压缩**：压缩时保留（或重放）`ToolSearchResultContent`，否则已发现的工具在原生路径下随之消失。
7. **用户身份接线**：auth 中间件替换固定 userID 约定。
