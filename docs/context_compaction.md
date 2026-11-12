# Context Compaction（上下文压缩）

> 落地状态：v1 已实现。本文是设计与决策记录，对应 architecture.md §3.2（Stage 3: Context Build）、
> §3.7（ContextEngine）与 §3.9（Budget 双维度）的 v1 落地形态。

## 问题

agent loop 的资源控制原本只有 `Budget.Exceeded`，判断的是本轮**累计消耗**（`TokenUsage` = 各次调用
`usage.Total` 累加，"时间维度"），不感知**当前上下文窗口占用**（"空间维度"）。`State.Messages`
全量恢复后只增不减，长对话中每次 LLM 调用都重发完整 transcript，最终超出模型 context window
导致调用失败。

## 核心决策

1. **压缩产物持久化到 `message_summaries` 表**，messages 原文永不改写（UI/API 读到的始终是完整历史）。
2. **压缩策略默认 LLM 总结**，策略接口可插拔（truncate / prune / hierarchical 留作后续演进）。
3. **v1 仅主动阈值触发**；provider 溢出错误兜底（类型化错误 + 强制压缩重试）留 v1.5。
4. **压缩调用走与主循环相同的 middleware 链**（计量/日志/重试无盲点），流上用新增的
   `summary` chunk type 标记，供链上 middleware 归因。
5. **摘要在视图中以 `SummaryContent` content kind 表示**（对齐 `ReasoningContent` 先例），
   渲染为模型可读文本是 per-provider 的适配点，业务侧不写死包裹格式。

## 触发

### 触发点

`iterate` 内、工具调用落库之后、`llm.start` 事件之前——即 architecture.md §3.2 的
"Stage 3: Context Build"。此时本轮 tool result 已 checkpoint，估算能覆盖最新增量。

### 触发条件（两级信号，无 tokenizer）

```
projected = State.LastInputTokens + estimateTokens(State.Messages[State.lastUsageAt:])
触发: projected >= threshold
```

- `LastInputTokens`：上一次 LLM 调用的 `UsageContent.Input`（provider 真实值，usage chunk 先于
  final chunk 是既有契约）；`lastUsageAt` 是记录时刻的消息数，之后新增的 assistant 消息与
  tool result 由增量估算覆盖。
- `estimateTokens`：`Σ(len(text)+len(arguments)+len(result)+len(encrypted)) / 4`。粗糙但只用于
  增量段，或在没有 usage 记录时（apiserver 每 turn 新建 Agent、压缩刚发生后）估算整个视图。
- `threshold` 解析顺序：
  1. `Budget.MaxContextTokens > 0` → 显式覆盖（architecture.md §3.9 语义）；
  2. 否则 `Catalog.Limit.Context × SafetyFactor`（默认 0.8，隐含为 output、工具定义、
     本轮增量预留余量）；
  3. 二者皆无（手写 catalog 无 limit 数据）→ 禁用并 `slog.Warn` 一次，绝不臆测窗口。

## 压缩执行

- **runner 装配**：与主链相同的用户 middleware + 最内层 `SummaryTagMiddleware`
  （`llm.NewRunner(options.LLM, append(options.Middlewares, llm.SummaryTagMiddleware)...)`）。
  tag middleware 把压缩流的 `final` chunk 重标为 `summary`（新增 chunk type，只此一种、无 delta；
  delta/usage 原样透传，usage 按调用边界天然归属）。压缩不产生 `llm.*` 总线事件；主循环的
  iterate 只消费主调用流，永远碰不到 `summary` chunk。压缩调用用非流式
  （`GenOptions.Stream=false`，流中仅 usage+final）。
- **默认策略 llmSummarizer**：把被丢弃段渲染成纯文本 transcript（每行一条：role 文本 /
  `assistant called tool(name, args)` / `tool result(name, is_error, result 截断 ~2k 字符)`），
  连同 `prior`（上次压缩的摘要文本，summary-of-summary 组合）拼成一条 user 消息发给 runner，
  提示词要求为"未来的自己"总结：用户目标、已做决定、工具动作与结果、涉及的文件、未完成任务。
  模型与主 agent 相同（v1）。
- **切割点选择**（保证 §3.7 的 tool_use/tool_result 配对不变量）：

```
start = 0; 若视图头部是上次的合成摘要消息则 start = 1
raw = len(messages) - KeepRecent
若 raw - start < minDrop(2) → 不压缩
cut = raw
while cut > start && !boundarySafe(messages, cut): cut--   // 向前回退
若 cut - start < minDrop → 不压缩

boundarySafe(cut): [0:cut) 内的 ToolCallContent.ID / ToolSearchCallContent.CallID 集合
与 [cut:] 内的 ToolResultContent.ToolCallID / ToolSearchResultContent.CallID 集合不相交
（result 总在 call 之后，反向情形不可能，无需对称检查）
```

  回退方向向前（把整对丢进摘要由 summarizer 叙述）而非向后（向后在 tool 密集尾部可能级联到
  什么都丢不掉）。被丢弃 assistant 消息的 `ReasoningContent.EncryptedContent` 随之消失；
  保留段消息绝不修改（compactor 构造新 slice）。
- **账务与熔断**：
  - 压缩消耗的 `usage.Total` 计入 `State.TokenUsage`（时间维度），失控压缩循环同时被
    `Budget.Exceeded` 终止；
  - 单 turn 压缩次数上限 `MaxPerTurn`（默认 5），超出置 `TurnStatusInterrupted` 并返回
    `ErrCompactionExhausted`；
  - never-shrink 保护：`estimateTokens(新视图) >= projected` → 丢弃结果继续；
  - 失败 fail-open：summarizer 出错 → `slog.Warn` + `runtime.error` 事件，不压缩继续跑
    （阈值是启发式，硬兜底属 v1.5）。

## 触发后的上下文管理

- **视图**：`State.Messages = [合成摘要消息] + 保留尾部`；`LastInputTokens, lastUsageAt = 0, 0`。
  合成消息 = `RoleUser + SummaryContent{Text}`，ID 为 `summary-<row id>`。
- **`SummaryContent`**：只携带 `Text`（覆盖范围等溯源信息在 message_summaries 行里，不进视图）；
  `Kind() = "summary"`，注册进 `UnmarshalMessageContents` 做防御性往返。各 provider adapter 负责
  渲染成该模型偏好的包裹格式：openai adapter 渲染为 `"[context summary]\n" + Text`；未来接
  claude 协议 adapter 时可换 `<summary>...</summary>` 等格式，业务侧零改动。识别靠类型断言
  （transcript renderer 跳过、`restoreToolCalls` 遇 RoleUser 即停），无 magic prefix 启发式。
- **恢复**（`restoreIfPresent`）：

```go
summary := Store.LoadLatestSummary(ctx, conversationID)     // 无则走原路径
messages := Store.LoadMessagesAfter(ctx, conversationID, summary.CoversToMessageID)
State.Messages = [summary.ToMessage()] + messages
a.lastSummary = summary                                      // 供下次压缩的 prior
```

- **summary-of-summary**：再次压缩时 `prior = a.lastSummary.Summary`，新摘要覆盖范围从视图头部
  （含旧摘要位）算到新 cut；旧 summary 行保留不动，`LoadLatestSummary` 按 id 取最新即天然取代。

## 数据层

`migrations/{sqlite,postgres}/01_messages.{up,down}.sql`：

```sql
CREATE TABLE message_summaries (
    id                      <自增主键>,          -- sqlite: INTEGER PRIMARY KEY AUTOINCREMENT; pg: BIGSERIAL
    conversation_id         TEXT NOT NULL,
    turn_seq                BIGINT NOT NULL,     -- 产生该摘要的 turn
    covers_from_message_id  TEXT NOT NULL,       -- 被覆盖首条消息的 message_id (uuid)
    covers_to_message_id    TEXT NOT NULL,       -- 被覆盖末条消息的 message_id (uuid)
    summary                 TEXT NOT NULL,
    tokens                  BIGINT NOT NULL DEFAULT 0,  -- 压缩调用的 token 消耗
    created_at              BIGINT NOT NULL      -- unix 秒，应用层赋值
);
CREATE INDEX ... ON message_summaries (conversation_id, id);
```

用 message uuid 而非新 seq 列标定覆盖范围：messages 表 PK `id` 自增即顺序，`covers_to_message_id`
在查询时解析成行 id（`WHERE conversation_id=? AND id > (SELECT id FROM messages WHERE
message_id=? ...)`），免去给 messages 表加列。architecture.md §4 草图的 `branch_id/run_id`
留待 Branch 特性引入。

`Repository` 接口新增（`LoadMessages` 保持不过滤，UI 会话详情要完整原文）：

```go
StoreSummary(ctx, turnContext *TurnContext, summary *persistent.SummaryModel) error
LoadLatestSummary(ctx, conversationID string) (*persistent.SummaryModel, error)   // 无则 nil
LoadMessagesAfter(ctx, conversationID string, afterMessageID string) ([]*underlying.Message, error)
```

## 观测

- 事件：`compact.start` / `compact.end`，payload
  `CompactPayload{ProjectedTokens, Threshold, DroppedMessages, KeptMessages, SummaryTokens, Compactions}`。
- tracer：`compact` span 作为 iteration span 子级（镜像 llm span 模式）。
- 计量：共享链上的 middleware 从 `summary` chunk type 归因压缩调用；压缩 usage 计入
  `State.TokenUsage` 并随 turn 落库。

## 成本考量

压缩调用与 title generation 的成本结构完全不同：输入是整段被丢弃的消息，最坏 ≈ 阈值
（0.8×105 万窗口 ≈ 84 万 token），输出仅摘要本身；transcript 是全新渲染、不命中 prompt cache，
单次压缩实际费用高于同刻一次主循环调用。但触发时主循环每次 iterate 本就在重发近窗口大小的输入，
压缩付一次同量级调用换来之后每轮输入降回 摘要+尾部——对长任务是净省钱，本质是让长任务跑完的
保险费。稳态频率 ≈ 每积累 0.8 窗新内容一次。控制手段：`Budget.MaxContextTokens` 压低阈值直接压
费用基线；v2 可换低价 summarizer 模型进一步降本。

## 非目标（v1 不做）

- provider `context_length_exceeded` 类型化错误 + 强制压缩重试（v1.5）。相关风险：summarizer
  自身输入 = 丢弃段，最坏 ≈ 阈值（默认 0.8×105 万 = 84 万，仍在 gpt-5-5 `limit.input` 92.2 万
  以内）；调高 SafetyFactor 导致 summarizer 自身超 `Limit.Input` 时，v1 走 fail-open。
  单次压缩输入上限（MaxInputTokens 旋钮）经评估 v1 不加，保持简单；
- `PreCompact` hook（现有 `hook.Input{Event, Tool}` 形状装不下压缩语义，v2 扩展 hook API 时一并做）；
- tool-result 裁剪、专用压缩模型、按 `Limit.Output` 细化预留量（v2 演进）。
