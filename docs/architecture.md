# LLM Agent Harness 框架设计方案

> 汇总从 middleware chain 出发，逐步演进到面向 harness 的完整 agent 框架的架构设计。

## 一、设计原则

1. **双层架构**：外层 Harness Loop 管控制流（state / tools / permissions / HITL），内层 Middleware Chain 只管一次 LLM 调用
2. **三种扩展机制各司其职**：Middleware（洋葱嵌套）/ Hook（并列 fanout）/ Event（异步广播）
3. **显式 State**：可序列化、可 checkpoint、可 resume、可 fork
4. **五层领域模型**：Conversation ⊃ Branch ⊃ Turn ⊃ Run ⊃ Iteration
5. **消息不可变**：编辑 = fork 新 branch，压缩产物独立存储不覆盖原文
6. **Event Sourcing**：过程可回放、UI 可实时、断线可补拉
7. **无状态恢复**：Harness 实例即用即弃，任意进程可从 DB 重建现场续跑（多实例 apiserver 的前提）

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
│   HookRegistry   EventBus        RunManager     Budget      │
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

**LLM 事件顺序约定**：`llm.usage` 必须先于 `llm.complete` 发出——消费方依赖此顺序（如 tracing 在仍处于打开状态的 LLM span 上记录 token 用量，收到 complete 即关闭 span）。对接新的 LLM 供应商时，Provider Adapter 必须保证 usage chunk 先于 final chunk 产出；若供应商在 final 之后才返回 usage，需在 adapter 侧调整顺序（现有 OpenAI adapter 在流式与非流式两种模式下均已保证）。

### 3.5 State 恢复（无独立 checkpoint 表）

**原则：消息日志是 source of truth，可推导的 state 不单独快照。** 对照 LangGraph checkpointer 的三层结构——逻辑上 MVCC（checkpoint 不可变、append-only、thread 持当前指针）、存储上 COW（按 channel version 结构共享）、time travel 是 fork 不是 rewind——本框架的内容侧已由 messages 表天然覆盖（append-only，粒度比 super-step 快照更细），因此**不建 checkpoints 表**。

| State                                            | 来源                                                                                |
| ------------------------------------------------ | ----------------------------------------------------------------------------------- |
| Messages / 待执行 ToolCalls / Skills / Activated | **推导**：messages 表（tool_call 配对关系即断点）                                   |
| turn 序号 / Status / Pending / 统计              | **落库**：turns + pending_approvals（控制面元数据，原地 UPDATE 不违反时间旅行语义） |

**恢复 = Load → Restore → 重入主循环**，不做状态机特殊跳转：

```
LoadMessages(conversation) → agent.Restore(messages) → Run / Resume
```

**落库粒度 = iteration 边界**（assistant final chunk 才写、tool result 成组写），库中状态永远一致，恢复点天然存在。

未来的 fork / time travel 走 Branch（fork_message_id），即 COW，不引入 checkpoint_id 寻址。

### 3.6 ToolRegistry + Skills

> Skill 的详细设计与落地实现见 [skills.md](skills.md)。下文的 Skill 结构是早期草图；实际实现中，显式选中的 skill 在 turn 开始前由 `beforeUserInput` 改写成 `Skill` 工具的 tool call + tool result 消息对并随之持久化，与模型主动加载（内置 `Skill` 工具的 tool result）形态一致。skill 目录的披露方式待 instructions 重设计后回归（见 skills.md）。

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

Registry 的 active set 在每个 turn 开始时计算一次；skill 目录的披露方式待 instructions 重设计后回归（见 skills.md）。

### 3.7 ContextEngine

**职责**：从 `messages` 表拉原文 + 已有 summaries + budget，产出 LLM 视图。

**关键不变量**：

- `tool_use` / `tool_result` 必须配对（Anthropic 严格，缺一个直接 400）
- `thinking` block 位置约束
- 压缩产物写 `message_summaries` 表，**不改 `messages` 表**

**压缩策略**：truncate / summarize / hierarchical memory / tool-result prune，可插拔。

### 3.8 PermissionPolicy + HITL

三种决策：`Allow` / `Deny` / `Ask`。与 Hook 合并：PreToolUse hook 优先，Policy 兜底。

**交互模式是 tool 的元数据**（注册时声明；审批场景不用枚举区分，由 tool schema 驱动）：

| 模式          | 含义                           | 人的产出（output）                                         |
| ------------- | ------------------------------ | ---------------------------------------------------------- |
| `None`        | 只读工具                       | —（永不挂起）                                              |
| `Approval`    | 副作用工具，授权后才执行       | 系统固定 decision schema：approve / deny / approveWithArgs |
| `Interactive` | 交互工具（AskUserQuestion 式） | 按工具声明的 output schema 校验后**直接作为 tool_result**  |

**混合场景（一轮多个 tool_call）**：

1. 分类（不执行）
2. 先执行所有 Allow（可并发，保序 append tool_result）
3. Deny 合成 error tool_result（**必须**，否则 API 400）
4. 遇到 Ask → 写 `pending_approvals`（tool_call_id 幂等键，input/output JSONB）→ 发 `permission.request` → turn 置 waiting_approval → Return ErrInterrupted

**等待期零资源**：无连接、无 goroutine、无内存状态。审批发现两通道：SSE 实时事件 + `GET /conversations/{id}` 资源表示。`expires_at` 过期 → 合成 error tool_result（配对不变量任何时刻不破）。

**Resume**：CAS 接管 turn → 决策 CAS 落库（`WHERE status='pending'`）→ 按工具元数据分流执行或直返 → 全部落 messages → 重入主循环。

**幂等**：`hasResult(tool_call_id)` 从 messages 派生，决策 CAS 防重复执行。

**已执行 Allow 不可撤销**：靠 tool 分级（ReadOnly vs 副作用）+ 保守模式（有 Ask 时 Allow 也 pending）解决。

**新 HITL 场景 = 注册新工具 + 声明 schema**，表结构与 resume 主流程零变更；与 MCP elicitation 的 `requestedSchema` 同构。

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

### 3.10 RunManager（实例内 run 监督者）

apiserver 多实例部署，每次请求 `agent.New` 新建实例，状态全部来自 DB（无 sticky session）。**两条生命周期分离**：

- **view**（SSE 订阅）：绑定请求 ctx，客户端断开即取消订阅；
- **run**（agent goroutine）：`context.WithoutCancel` 派生（保留 TurnContext / trace values），由 RunManager 持有 cancel，与任何连接无关。

**职责**：

1. run 注册表（turnID → cancel）
2. 心跳续租 + 读回 `cancel_requested`
3. goroutine recover（脱离 HTTP 栈后 panic 无人兜）
4. 退出必写终态（done / failed / waiting_approval / interrupted）
5. 优雅停机：拒新 turn → 等存量 → 超时 cancel 标 interrupted

**终止条件**：done / budget（客户端消失时的兜底上界）/ suspend / cancel / 僵尸回收（见 5.6）。

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
turns(id, conversation_id, branch_id, turn_number, user_message_id, status,
      lease_until, epoch, cancel_requested,           -- 多实例控制面，见 5.6
      iterations, tokens_used, last_message_id, ...)
runs(id, turn_id, agent_id, parent_run_id, handoff_from_run, root_message_id,
     status, trace_id, iteration_count, tokens_used, ...)    -- MVP 与 turns 1:1 并存
messages(id, conversation_id, branch_id, turn_id, run_id, parent_message_id,
         role, content, tool_calls, tool_call_id, seq, is_sub_agent_internal, ...)
events(id BIGSERIAL, run_id, turn_id, iteration_index, type, tier, payload,
       trace_id, span_id, parent_event_id, ...)
pending_approvals(id, conversation_id, turn_id, tool_call_id UNIQUE,      -- 幂等键
                  tool_name, input JSONB, output JSONB, reason,           -- 场景由 tool schema 驱动，见 3.8
                  status, expires_at, created_at, decided_at)
message_summaries(id, conversation_id, branch_id, run_id,
                  covers_from_seq, covers_to_seq, summary, tokens, ...)
```

**Turn : Run = 1 : N**（关键）：

- 单 agent：1 : 1
- Sub-agent：1 : N（嵌套，parent_run_id 关联）
- Handoff：1 : N（平级，handoff_from_run 关联）
- Retry：1 : N（同 root_message_id）
- Guardrail 拦截：1 : 0

**MVP 简化**：Turn : Run 按 1 : 1 实现，run 级控制字段（lease / epoch / 统计）先并存在 turns 表，sub-agent / retry 引入时再拆出 runs。原设计中的 `checkpoints` 表与 `runs.state_snapshot` 已移除：state 从 messages 推导（见 3.5），只有控制面落库。

---

## 五、关键流程

### 5.1 用户投递输入（统一 endpoint）

客户端心智：永远只向 conversation 资源投递输入、接收事件，**不理解服务端状态机**。

```
POST /conversations/{id}                      唯一投递口，SSE 响应
  { type: "message",  text, model, skills, message_id }   ← message_id 客户端生成，投递幂等
  { type: "approval", decisions: [{approval_id, action, updated_args?}] }
  { type: "cancel" }
```

服务端按 `body.type` + 当前 turn 状态分流（显式 type 判别，混合 payload 一律 400）：

| body     | turn 状态         | 行为                                                                                   |
| -------- | ----------------- | -------------------------------------------------------------------------------------- |
| message  | 空闲              | BeginTurn → Run                                                                        |
| message  | running           | 409                                                                                    |
| message  | waiting_approval  | **带反馈的拒绝**：deny 全部 pending，text 作为反馈进 error tool_result，同一 turn 续跑 |
| approval | waiting_approval  | Resume（见 5.2）                                                                       |
| approval | 其他              | 409                                                                                    |
| cancel   | running / waiting | 置 cancel_requested（见 5.6）                                                          |

`GET /conversations/{id}` 的资源表示携带 `pending_approvals`，客户端唯一规则：渲染当前挂着的审批。

Run 在后台异步推进（RunManager，见 3.10），事件经 SSE 推送；Run 结束发 `done` sentinel 后客户端断开。

### 5.2 HITL 挂起与恢复

```
LLM 返回 [A(Allow), B(Ask), C(Allow)]
  ├─ 执行 A、C，append tool_result
  ├─ B → INSERT pending_approvals（tool_call_id 幂等键）
  ├─ Emit permission.request
  ├─ turns.status = waiting_approval
  ├─ Return ErrInterrupted（goroutine 退出，零资源持有）
  └─ SSE 发 turn.suspended 后正常关闭

（用户决策可能数小时后，经任意实例到达）

POST /conversations/{id}  { type: "approval", decisions: {...} }
  ↓
断言 waiting → CAS 接管（epoch+1，见 5.6）→ 决策 CAS 落库（幂等）
  ├─ approve → 执行 tool（可用 updated_args）→ append tool_result
  ├─ deny → 合成 error tool_result
  └─ Interactive 工具 → output 按 schema 校验，直作 tool_result
  ↓
status = running → 重入主循环，事件走本次请求的新 SSE
```

**Deny 必须合成 error tool_result**（API 层配对不变量）。部分决策 → 保持 waiting；全部决策 → 续跑。

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

### 5.6 多实例：租约、fencing 与故障恢复

**活性**：`lease_until` + 心跳续租。

**防脑裂（fencing epoch）**：接管 turn 时 `epoch+1`；持有方所有写带 `WHERE epoch=?`，失配即自我取消。消息持久化事务内嵌 epoch 断言（兼作租约续期），fencing 窗口压到一个事务内：

```
接管: UPDATE turns SET epoch=epoch+1, lease_until=? WHERE id=? AND lease_until < now
写入: UPDATE turns SET lease_until=? WHERE id=? AND epoch=?   -- 与 StoreMessages 同事务
```

**跨实例取消**：cancel 是数据（`turns.cancel_requested`）。请求落到持有实例 → 内存 registry 直接 cancel；否则心跳时读回标志执行。

**僵尸回收**：租约过期 → 标 interrupted。MVP 由用户显式重试；自动续跑待副作用工具支持幂等键（tool_call_id 天然可用）后放开——崩溃窗口"工具已执行、result 未落库"无法靠消息粒度幂等消除。

**事件跨实例**：MVP 期 run 在收请求的实例执行、SSE 连同一实例（按 conversation 一致性路由）；长期 Redis Streams fanout（见 §8）。

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

### Phase 2 — 恢复 + HITL + 多实例

1. turns 表 + LoadMessages + Restore（第 N 轮恢复）
2. pending_approvals + Suspend / Resume + 统一 endpoint（message / approval / cancel）
3. RunManager（run / view 生命周期分离 + cancel）
4. 租约 / epoch fencing / 僵尸回收 + 审批过期
5. events 表 + SSE Last-Event-ID 补拉

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

### Backlog

- **warm agent pool**（单机 / 桌面）：turn 结束或挂起后实例置 idle + TTL 回收，resume 命中 warm 实例可跳过 store 重连与 state 重建；与多实例模型互斥（会把 turn 钉在特定节点），默认关闭，多实例下靠共享连接池 / skill 缓存优化冷启动
- **MCP elicitation**（工具执行中索取输入）：需工具可挂起 / 可重入；可复用 pending_approvals（Interactive），resume 变为"带补充输入重入执行"
- **崩溃自动续跑**：需副作用工具支持幂等键（tool_call_id 天然可用）；放开前僵尸一律 interrupted + 用户显式重试

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
11. **不建 checkpoints 表**：messages append-only 即 MVCC，state 靠推导，只有控制面落库
12. **活性靠 lease，防脑裂靠 epoch fencing**：fencing 断言内嵌消息写事务
13. **等待期零资源**：HITL 挂起即释放连接与 goroutine，审批是数据不是回调
14. **统一投递口**：客户端不理解状态机；waiting 时收到 text = 带反馈的拒绝
15. **审批场景由 tool schema 驱动**：交互模式是工具元数据，新场景零表变更（与 MCP elicitation 同构）

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
