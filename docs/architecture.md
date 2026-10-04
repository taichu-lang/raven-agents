# LLM Agent Harness 框架设计方案

> 汇总从 middleware chain 出发，逐步演进到面向 harness 的完整 agent 框架的架构设计。

## 一、设计原则

1. **双层架构**：外层 Harness Loop 管控制流（state / tools / permissions / HITL），内层 Middleware Chain 只管一次 LLM 调用
2. **三种扩展机制各司其职**：Middleware（洋葱嵌套）/ Hook（并列 fanout）/ Event（异步广播）
3. **显式 State**：可序列化、可 checkpoint、可 resume、可 fork
4. **五层领域模型**：Conversation ⊃ Branch ⊃ Turn ⊃ Run ⊃ Iteration
5. **消息不可变**：编辑 = fork 新 branch，压缩产物独立存储不覆盖原文
6. **Event Sourcing**：过程可回放、UI 可实时、断线可补拉

---

## 二、整体架构

```
┌─────────────────────────────────────────────────────────────┐
│                       用户交互层                              │
│   Web UI / CLI / API   ←→   SSE Streamer (Last-Event-ID)    │
└──────────────────────────────┬──────────────────────────────┘
                               │
┌──────────────────────────────┴──────────────────────────────┐
│                   Harness Loop（外层）                        │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ Stage 1: SessionStart / UserPromptSubmit hooks         │ │
│  │ Stage 2: Tool 激活（Skills 动态注入）                    │ │
│  │ Stage 3: Context Build（PreCompact hook / 压缩）        │ │
│  │ Stage 4: LLM Call → Middleware Chain（内层）            │ │
│  │ Stage 5: Stop hook / 终止判断                          │ │
│  │ Stage 6: PreToolUse → Permission → Exec → PostToolUse  │ │
│  │ Stage 7: Checkpoint + Event Emit                       │ │
│  └────────────────────────────────────────────────────────┘ │
│                         ↓ 嵌套                               │
│                Sub-Harness（spawn_task tool）                 │
└──────────────────────────────┬──────────────────────────────┘
                               │
┌──────────────────────────────┴──────────────────────────────┐
│                       基础设施层                              │
│   ToolRegistry   ContextEngine   PermissionPolicy           │
│   HookRegistry   EventBus        CheckPointer   Budget      │
└──────────────────────────────┬──────────────────────────────┘
                               │
┌──────────────────────────────┴──────────────────────────────┐
│                        持久化层                               │
│   Postgres: conversations / branches / turns / runs         │
│             messages / events / checkpoints / summaries     │
│   Object Storage: 冷 event 归档                              │
└──────────────────────────────┬──────────────────────────────┘
                               │
┌──────────────────────────────┴──────────────────────────────┐
│                        观测层                                 │
│   Tracing (OTel/Phoenix)   ←  EventBus Subscribers          │
│   Metrics (Prometheus)     ←                                │
│   Audit Log                ←                                │
└─────────────────────────────────────────────────────────────┘
```

---

## 三、核心组件

### 3.1 Middleware Chain（内层：LLM 调用管线）

**只包一次 LLM 调用**，洋葱式嵌套。所有 middleware 满足 `func(next) → handler` 签名：

- Logging / Tracing
- Retry / Timeout
- Provider Adapter（OpenAI / Anthropic / Gemini / 本地模型）
- Stream Parser（把 SSE chunk 拼装成 `ResponseChunk`）
- Rate Limit / Cache

**不含**：工具执行、loop 控制、权限判断——这些都在外层。

### 3.2 Harness Loop（外层：agent 主循环）

每个 iteration 7 个阶段，每阶段都可挂 hook：

```go
for !Budget.Exceeded(State) {
    // Resume 分支：如果之前挂在等审批
    if State.Status == WaitingApproval { ... }

    // 1. Session/User hooks（首次或有新 user input 时）
    // 2. Tool 激活（Skills 动态计算 active set）
    // 3. Context Build（触发压缩、注入 additionalContext）
    // 4. Middleware Chain 调 LLM
    // 5. Stop hook（可 block 强制继续）
    // 6. 逐个 tool call：PreToolUse → Permission → Exec → PostToolUse
    // 7. Checkpoint + Emit events
}
```

**Loop 是可重入的**：从 `State.Status` 判断入口分支，Resume 直接调 `Run(ctx)` 就能续跑。

### 3.3 Hook Registry（9 个命名扩展点）

对齐 Claude Agent SDK：

| Hook           | 时机                   | 能力                                      |
| -------------- | ---------------------- | ----------------------------------------- |
| `TurnStart`    | 会话初始化 / resume    | 注入 `additionalContext` 到 system prompt |
| `UserInput`    | 用户提交后、进 loop 前 | block / 注入 context / 改写 prompt        |
| `PreCompact`   | 触发压缩前             | 拦截 / 换策略 / 保护特定消息              |
| `PreToolUse`   | tool 执行前            | approve / deny / ask / 改 args / 换 tool  |
| `PostToolUse`  | tool 执行后            | 注入 feedback（lint 错误等）给模型看      |
| `Notification` | 需要通知用户时         | 转发 IM / 桌面通知                        |
| `Stop`         | 模型准备停止时         | **block → 强制继续**（自我反思机制）      |
| `SubagentStop` | 子 agent 停止时        | 决定是否让主 agent 接手                   |
| `TurnEnd`      | 会话结束               | 清理 / 落盘 / 上报                        |

Hook 特性：

- 并列 fanout + 决策合并（deny 短路、context 累加、updatedInput 按 priority）
- 支持 in-process function **和** shell command（stdin/stdout JSON）
- Fail-open / Fail-closed 按类型分（安全类 fail-closed，观测类 fail-open）
- 超时保护（默认 30s）

### 3.4 EventBus（观察总线，分层）

| Tier        | 事件示例                                                  | 持久化                     | 背压             | 消费者                             |
| ----------- | --------------------------------------------------------- | -------------------------- | ---------------- | ---------------------------------- |
| **Control** | `tool_call.*`, `permission.*`, `iteration.*`, `run.*`     | 必须（Postgres BIGSERIAL） | Block / KickSlow | Persister / SSE / Audit / Langfuse |
| **Data**    | `message.delta`, `thinking.delta`, `tool_call.args_delta` | 可选（内存 seq）           | DropOldest       | UI 实时流                          |

**Publish 顺序**：persist → fanout（不能反过来，否则 SSE 客户端收到 ID 但查库查不到）。

**Filter 订阅**（不是 topic）：

```go
Filter{RunIDs: {...}, Types: {...}, TypePrefixes: ["tool_call."], Tier: &Control}
```

**慢消费者 4 种策略**：DropNewest / DropOldest / Block / KickSlow（SSE 默认踢掉让客户端重连补拉）。

**顺序保证**：per-run 单调（loop goroutine 串行 publish），跨 run 不保证。

### 3.5 Checkpointer

**落点三处**：

1. 每轮 iteration 结束
2. HITL 挂起前
3. Resume 决策后

**Resume 流程**：

```
Load 最近 checkpoint → Replay events after → applyEvent 重建 state → new Harness → Run/Resume
```

**跨 HTTP 请求**：Harness 是无状态实例，State 是持久化的，任何进程 load 都能续跑。

### 3.6 ToolRegistry + Skills

Tool 元信息：

```go
type Tool struct {
    Spec          ToolSpec
    ReadOnly      bool                              // 策略参考
    Activate      func(*AgentState) bool            // Skill 激活条件
    NeedsApproval func(args json.RawMessage) bool   // 静态审批规则
    Exec          func(ctx, args) (string, error)
}
```

**Skill = 一组 Tool + Prompt 片段 + Activation**，运行时可插拔：

```go
type Skill struct {
    Name        string
    PromptFrag  string
    Tools       []*Tool
    Activate    func(*AgentState) bool
}
```

Registry 每轮 iteration 重新计算 active set，system prompt 动态拼接。

### 3.7 ContextEngine

**职责**：从 `messages` 表拉原文 + 已有 summaries + budget，产出 LLM 视图。

**关键不变量**：

- `tool_use` / `tool_result` 必须配对（Anthropic 严格，缺一个直接 400）
- `thinking` block 位置约束
- 压缩产物写 `message_summaries` 表，**不改 `messages` 表**

**压缩策略**：truncate / summarize / hierarchical memory / tool-result prune，可插拔。

### 3.8 PermissionPolicy + HITL

三种决策：`Allow` / `Deny` / `Ask`

**与 Hook 合并**：PreToolUse hook 优先，Policy 兜底。

**混合场景（一轮多个 tool_call）**：

1. 分类（不执行）
2. 先执行所有 Allow（可并发，保序 append tool_result）
3. Deny 合成 error tool_result（**必须**，否则 API 400）
4. 遇到 Ask 挂起 → Checkpoint → Return ErrInterrupted

**Resume 三种决策**：Approve / ApproveWithArgs / Deny，每种都必须落到 messages 里。

**幂等**：`hasResult(tool_call_id)` 从 messages 派生，不用单独 flag。

**已执行 Allow 不可撤销**：靠 tool 分级（ReadOnly vs 副作用）+ 保守模式（有 Ask 时 Allow 也 pending）解决。

### 3.9 Budget

```go
type Budget struct {
    MaxTurns          int           // 防死循环
    MaxContextTokens  int           // 单次窗口 → 触发压缩
    MaxTotalTokens    int           // 累计消耗 → 触发终止
    MaxCostUSD        float64       // 累计金额（比 tokens 更直接）
    MaxWallclock      time.Duration
    ReservedOutput    int           // 给 completion 预留
}
```

Context tokens 是**空间维度**（触发压缩），Total tokens 是**时间维度**（触发终止），两者独立。

---

## 四、数据模型（五层领域）

```
Conversation  用户视角的一整个对话（跨天跨设备）
  │
  ├── Branch  fork/edit 产生的分支（append-only）
  │     │
  │     └── Turn  一次「用户输入 → agent 完成」（用户视角单位）
  │           │
  │           └── Run  一次 agent 执行（系统视角单位，checkpoint/HITL/trace 挂这）
  │                 │       可嵌套（parent_run_id）、可 handoff（handoff_from_run）
  │                 │
  │                 └── Iteration  agent loop 一轮（1 LLM call + N tool calls）
  │                       │
  │                       └── Message / Event  最细粒度
```

### 命名约定

| 概念         | 用这个         | 避免                                |
| ------------ | -------------- | ----------------------------------- |
| 用户视角对话 | `Conversation` | Session（留给认证层）、Chat、Thread |
| 一问一答     | `Turn`         | Round、Exchange、Step               |
| Agent 执行   | `Run`          | Task、Execution、Job                |
| Loop 一轮    | `Iteration`    | Step、Cycle                         |
| 分支         | `Branch`       | Fork（动作）、Path、Version         |

### 表结构（Postgres）

```sql
conversations(id, user_id, title, active_branch, config, ...)
branches(id, conversation_id, parent_branch_id, fork_message_id, head_message_id, ...)
turns(id, conversation_id, branch_id, turn_number, user_message_id, status, ...)
runs(id, turn_id, agent_id, parent_run_id, handoff_from_run, root_message_id,
     status, trace_id, state_snapshot, iteration_count, tokens_used, ...)
messages(id, conversation_id, branch_id, turn_id, run_id, parent_message_id,
         role, content, tool_calls, tool_call_id, seq, is_sub_agent_internal, ...)
events(id BIGSERIAL, run_id, turn_id, iteration_index, type, tier, payload,
       trace_id, span_id, parent_event_id, ...)
checkpoints(run_id, at_event_id, iteration_index, state, ...)
message_summaries(id, conversation_id, branch_id, run_id,
                  covers_from_seq, covers_to_seq, summary, tokens, ...)
```

**Turn : Run = 1 : N**（关键）：

- 单 agent：1 : 1
- Sub-agent：1 : N（嵌套，parent_run_id 关联）
- Handoff：1 : N（平级，handoff_from_run 关联）
- Retry：1 : N（同 root_message_id）
- Guardrail 拦截：1 : 0

---

## 五、关键流程

### 5.1 用户发送消息（正常流程）

```
POST /conversations/{id}/messages
  ↓
创建 Turn + user Message + Run
  ↓
异步启动 Harness.Run(ctx)
  ↓
前端订阅 GET /runs/{run_id}/stream (SSE)
  ↓
Loop iteration → emit events → SSE push
  ↓
Run done → SSE 'done' sentinel → 客户端 abort
```

### 5.2 HITL 挂起与恢复

```
LLM 返回 [A(Allow), B(Ask), C(Allow)]
  ├─ 执行 A、C，append tool_result          Checkpoint #1
  ├─ B → Pending, Status=WaitingApproval
  ├─ Emit permission_request
  ├─ Checkpoint.Save                        Checkpoint #2
  └─ Return ErrInterrupted

（用户 UI 决策，可能几分钟后）

POST /runs/{id}/resume  body: {decisions: {B.id: {kind: "approve"}}}
  ↓
Load checkpoint → rebuild Harness → Resume(decisions)
  ├─ Approve → execTool(B)
  ├─ Deny → appendToolResult(B, "denied: ...", true)
  └─ ApproveWithArgs → 用新 args 执行
  ↓
Status = Running → 回主 loop
```

**Deny 必须合成 error tool_result**（API 层配对不变量）。

### 5.3 Fork（从历史点继续）

```
POST /messages/{msg_id}/fork  body: {content: "改写后"}
  ↓
新 branch_id + 新 user message (parent = 原消息的 parent, sibling of 原消息)
  ↓
从 parent 之前的 messages 重建 state_snapshot
  ↓
新 Turn + 新 Run，原 branch 不动
```

### 5.4 Sub-agent（context isolation）

```
Main agent 调用 spawn_task tool
  ↓
Tool.Exec: New Harness(fresh state, subset tools, same infra)
  ↓
Sub.Run(ctx) 独立跑完
  ↓
只把 sub 的 final message 作为 tool_result 返回主 agent
  ↓
Sub 内部 messages 存 messages.is_sub_agent_internal=TRUE（UI 默认不展示）
```

**Sub-agent ≠ Multi-agent workflow**，是 context budget 管理原语。

### 5.5 SSE 断线重连

```
客户端记住 Last-Event-ID
  ↓
重连时带 header
  ↓
服务端: Subscribe(filter) → 记录 subStartID
  ↓
Replay(run_id, lastID) 补拉断线期间事件
  ↓
Dedup by ID（跳过 <= lastID 的）
  ↓
增量推送
```

**首次连接推 snapshot**（比逐条重放快），后续走增量。

---

## 六、观测层

### Trace 层级（OTel / Langfuse）

```
Trace (trace_id = conversation_id)
└── Span: turn.<n>
    └── Span: run.<agent_id>
        └── Span: iteration.<i>
            ├── Span: llm.call          ← Middleware 建
            │   └── Generation          ← Langfuse 特有
            └── Span: tool.<name>       ← TracedTool decorator 建
                └── Span: run.<sub>     ← 嵌套 sub-agent
```

### EventBus Subscribers

- **Persister**（同步内嵌，不是普通 subscriber）
- **SSE Streamer**（per-client，KickSlow）
- **Trace Exporter**（Control tier，Block）
- **Prometheus Metrics**（DropNewest）
- **Audit Logger**（Control tier，Block）
- **WebSocket Bridge**（跨服务广播）

---

## 七、三种扩展机制的分工

| 抽象           | 位置         | 组合方式           | 干预能力                             | 类比                          |
| -------------- | ------------ | ------------------ | ------------------------------------ | ----------------------------- |
| **Middleware** | LLM 调用内部 | 洋葱嵌套           | 修改 request/response，可跳过 next   | AOP around advice             |
| **Hook**       | Loop 决策点  | 并列 fanout + 合并 | 拦截 / 决策 / 改 args / 注入 context | Webpack / VSCode plugin hooks |
| **Event**      | 全流程广播   | 异步 subscribe     | 只观察不干预                         | DOM events / Kafka            |

三者**共享同一次业务动作**，各自服务不同消费者：

- Middleware 修改的是 **LLM 调用本身**
- Hook 修改的是 **loop 的决策路径**
- Event 修改的是 **外部世界的视图**（UI / 日志 / 监控）

---

## 八、技术选型建议（Go 生态）

| 层                   | 推荐                            | 备注                                      |
| -------------------- | ------------------------------- | ----------------------------------------- |
| Web 框架             | Chi / Gin                       | Chi 更贴近 net/http                       |
| DB                   | Postgres                        | events 表按 run_id 分区                   |
| 缓存 / 跨进程 fanout | Redis Streams                   | 多实例部署时用                            |
| Tracing              | Phoenix + OTel                  | [Phoenix](https://arize.com/docs/phoenix) |
| 前端 SSE             | `@microsoft/fetch-event-source` | onclose/onerror 必须 throw 防重连         |
| 反代                 | Caddy                           | `flush_interval -1` + `read_timeout 0`    |
| 参考框架             | Eino (ByteDance)                | Go 生态最完整，Graph + Chain              |

---

## 九、实现路线图

### Phase 1 — MVP

- Middleware Chain（Logging / Retry / Provider Adapter）
- 单 agent Harness Loop（无 hook）
- ToolRegistry（静态注册）
- Postgres：conversations / messages / runs
- SSE Streamer（无 Replay）

### Phase 2 — HITL + 持久化

- Checkpointer
- PermissionPolicy + Pending 挂起
- Resume API（Approve / Deny / Modify）
- Events 表 + SSE Last-Event-ID 补拉

### Phase 3 — 扩展性

- HookRegistry（9 个事件点）
- Skills（动态激活 + prompt 注入）
- ContextEngine 压缩（PreCompact hook）
- Shell command hook 支持

### Phase 4 — Multi-agent

- Sub-harness（spawn_task tool）
- Handoff 支持（parent_run_id / handoff_from_run）

### Phase 5 — 用户功能

- Branch / Fork
- 消息编辑重跑
- Conversation 归档
- 冷数据分层存储

---

## 十、关键取舍

1. **Message 不可变**：编辑 = fork，永不 UPDATE
2. **压缩产物独立表**：不覆盖原文，UI 永远看原文
3. **Persister 内嵌 Publish**：同步落库保证 SSE 补拉的 ID 一致性
4. **Sub-agent 是 context isolation，不是 workflow**：不引入 DAG 编排
5. **Data tier 高频事件不落库**：避免 events 表爆炸（一次 run 几千条 token delta）
6. **Loop 内 publish 同步**：保证事件顺序 = 业务顺序
7. **Hook fail-open / fail-closed 按类型分**：安全类严格，观测类宽松
8. **Bus 不做全局排序**：单 run 串行天然有序，跨 run 无序（consumer 按 ID 自排）
9. **Deny 必须合成 error tool_result**：API 层配对不变量
10. **LangGraph 是状态机层，不是 harness 层**：只借鉴 checkpointer / interrupt 机制，别照搬 graph 抽象

---

## 十一、参考实现

**Harness 层（重点）**：

1. **Claude Agent SDK** — hook 命名 / skills / sub-agent / permission mode
2. **OpenHands** — 完整开源 harness 参考（event stream + agent runtime）
3. **OpenAI Agents SDK** — Guardrails / Sessions / Tracing

**状态机层（工具书）**：4. **LangGraph** — checkpointer / interrupt / time-travel 源码 5. **Eino (ByteDance)** — Go 生态最完整

**前端参考**：6. **Cline** — VSCode 扩展，审批 UI / tool 分级 / context 管理

---

## 一句话总结

**双层（Loop + Chain）+ 三扩展（Middleware / Hook / Event）+ 五层模型（Conversation / Branch / Turn / Run / Iteration）+ Event Sourcing**。业务代码只写一次动作，控制流走 Loop、LLM 调用走 Chain、决策点走 Hook、观察走 Event，四个视角自然分离，可独立演进。
