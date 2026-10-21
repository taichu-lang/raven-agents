# Skill 设计

> 本文描述 Skill 的落地设计与实现。总体架构见 [architecture.md](architecture.md) 的 3.6 节。

## 一、目标

Skill 是一段可复用的指令（一份 `SKILL.md`），用来教模型如何完成某类任务。本设计要解决两件事：

1. **显式选择**：用户在发消息时勾选 skill，agent 在 turn 开始前把每条选择改写成 `Skill` 工具的 tool call + tool result 消息对，全文随 tool result 进入对话。
2. **模型自主**：模型判断任务需要某个 skill 时，通过 `Skill` 工具按名拉取全文。

两种场景**混合生效**，且对模型呈现**完全相同的形态**：一条 assistant 的 function call，跟一条承载全文的 tool result。skill 目录（name + description）的披露机制留待下一版本随 instructions 重设计回归（见 6.1）。

本期范围：显式选择以消息对形态在 turn 开始前进入对话，不做目录披露。skill 自带的工具（allowed-tools）与脚本暂不支持，但结构上已预留扩展点（见第九节）。

## 二、两种场景

|                    | 显式选择（eager）                                                          | 模型自主（lazy）                           |
| ------------------ | -------------------------------------------------------------------------- | ------------------------------------------ |
| 触发               | `request.metadata.skills` 非空                                             | 模型判断任务匹配后调用 `Skill` 工具        |
| 模型看到的内容     | tool result 中的全文                                                       | tool result 中的全文，形态与左侧完全一致   |
| 全文何时进入上下文 | turn 开始前（`beforeUserInput` 把选择改写成消息对）                        | 模型调用 `Skill` 工具之后                  |
| 落库形态           | assistant tool call + tool result 消息对，随 turn 的消息写入 `messages` 表 | 同形态；工具结果的落库尚未接上（见第九节） |
| 重复披露           | 不会：全文已在对话中                                                       | 不会：已拉取的全文留在对话里               |

## 三、组件

| 组件                              | 位置                              | 职责                                                                          |
| --------------------------------- | --------------------------------- | ----------------------------------------------------------------------------- |
| `Skill` / `Loaded`                | `agent/skill/skill.go`            | 元信息；`Loaded` 额外携带正文                                                 |
| `Loader`                          | `agent/skill/loader.go`           | 扫描目录、解析 frontmatter、读取正文、缓存                                    |
| `SkillRegistry`                   | `agent/skill.go`                  | 进程级已安装 skill 表，与 `ToolRegistry` 同构                                 |
| `Skill` 工具                      | `agent/skill.go`                  | 内置工具，模型用它按名字加载全文                                              |
| `SkillContent`                    | `agent/llm/underlying/content.go` | 选择标记：调用方在输入中用它表示"本 turn 使用某 skill"，只含名字，不含正文    |
| `beforeUserInput`                 | `agent/hooks.go`                  | turn 开始前把每条选择消息改写成 `Skill` 工具的 tool call + tool result 消息对 |
| `AgentState.Skills` / `Activated` | `agent/state.go`                  | 本轮显式选中的名字 / 模型已拉取的名字                                         |

### 依赖方向

```
handler → application → agent → agent/skill
                          ↓
                 agent/llm/underlying ← agent/llm/openai
```

- `agent/skill` 只被 `agent`、`apiserver/domain`（请求字段）与 `apiserver/handler`（构建 registry）引用，自身不依赖 agent 的其他部分。
- `underlying` 不依赖 `agent/skill`：`SkillContent` 只携带名字，由 `agent` 包解析成消息对。
- `openai` 只依赖 `underlying`，不感知 skill 机制：选择在到达适配层之前已被改写为普通的 function call / function call output。

## 四、Skill 目录约定

```
<app_dir>/.agents/skills/
└── pdf-processing/
    └── SKILL.md
```

`SKILL.md` 由 YAML frontmatter 与正文组成：

```markdown
---
name: pdf-processing
description: Fill in PDF forms and extract text
version: "1.0"
---

# PDF processing

When the user asks to fill a PDF form, ...
```

规则：

- `name` 缺省时取目录名；`description` 与 `version` 可选。
- 只扫描 `<app_dir>/.agents/skills` 的直接子目录；没有 `SKILL.md` 的目录静默跳过。
- frontmatter 解析失败的 skill 跳过并记录告警，不影响其他 skill 加载。
- 正文上限 1 MiB（`skill.DefaultMaxSize`）。

## 五、数据流

### 5.1 启动

```
Config.AppDir + ".agents/skills"
   └─► Loader.Scan(dir)            解析所有 SKILL.md 的元信息，并登记 root
        └─► SkillRegistry.Register(skill, nil)   进程级，所有请求共享
```

扫描失败只记录告警，服务照常启动（无 skill）。

### 5.2 一次请求

```
POST /api/v1/chat/:id   body.metadata.skills = [{name: "pdf-processing"}]
  │
  ├─ ChatService.selectedSkills
  │     SkillRegistry.Resolve(names)      未知名字 → ErrSkillNotFound → HTTP 400
  │
  ├─ agent.New(..., SkillRegistry)        注册内置 Skill 工具
  │
  └─ agent.Run(ctx, input)                input 携带文本消息 + 每个选中 skill 一条选择消息（只含名字）
        │
        ├─ beforeUserInput:
        │    └─ 每条选择消息 → registry.Load → 改写为 assistant tool call + tool result 消息对
        │         成功 → 名字记入 State.Skills，选择消息本身不进入对话
        │         失败 → Run 返回错误，turn 终止
        │    消息对与文本消息一起写入 messages 表
        │
        └─ 每次 iterate:
             ├─ invokeToolCalls           执行上一轮的工具调用
             │    └─ 成功的 Skill 调用 → State.Activated 记账
             └─ ToolRegistry.Active(state)     turn 开始时计算一次可用工具
```

### 5.3 模型自主加载

```
iteration N:   工具含 Skill；模型已知某个 skill 的名字（用户在文本中给出，或来自其他上下文）
               模型调用 Skill{name: "alpha"}
iteration N+1: invokeToolCalls
                 ├─ Skill 工具返回 alpha 全文（作为 tool 消息加入 State.Messages）
                 └─ State.Activated += alpha
```

产生的消息对与显式选择改写出的消息对完全同构，模型无法区分两种来源。

## 六、关键决策

### 6.1 注入机制已移除（第一阶段）

早期的设计把 skill 目录与选中全文由 `ContextManager.Build` 在每个 iteration 合成 user message 注入（插在最后一条 user message 之前）。该机制已整体删除：turn 的上下文就是消息本身，skill 全文只随 `Skill` 工具的 tool result 出现。

skill 目录的披露与 instructions 的定制（`ProviderOptions.Instructions` 目前是单个字符串，可能改为 array）留待下一版本一并设计。

### 6.2 显式选择改写成消息对

选择消息只携带名字（`SkillContent`），不是对话内容：`beforeUserInput` 在 turn 开始前把它替换成 `Skill` 工具的 tool call + tool result 消息对，与模型自主拉取产生的消息对完全同构。因此两种场景对模型不可区分，协议适配层也不需要任何 skill 专属的渲染逻辑；选择消息本身不进入对话、不落库。

tool result 的内容与 `invokeToolCalls` 对所有工具输出使用同一编码（见 6.4），保证两种路径的工具结果逐字节一致。

`AgentState.Skills` 只保存名字。正文随消息对持久化在 `messages` 表，state 里不重复存放正文，保持 checkpoint 轻量。名字用于将来 skill 自带工具的激活条件（`Contains(Skills) || Contains(Activated)`）。

### 6.3 工具是无状态的

`Skill` 工具只闭包 `SkillRegistry`，不闭包 agent 或 state。原因：

- 同一 iteration 的工具调用是并发执行的，闭包写 state 会产生数据竞争。
- registry 可以在多个 agent 之间共享。

“哪些 skill 已被模型加载”由 agent 在 `wg.Wait()` 之后、单 goroutine 中记账（`recordActivatedSkills`）。

### 6.4 工具结果为字符串

`functool` 的输出 schema 校验不接受 Go struct（jsonschema-go 的限制，见 issue #23），因此 `Skill` 工具直接返回正文字符串；`invokeToolCalls` 统一对其做 JSON 编码后写入 tool result。显式选择的消息对复用同一编码。

### 6.5 active set 每 turn 计算

`ToolRegistry.Active` 在每个 turn 开始时计算一次（不是每个 iteration）。目录为空时 `Skill` 工具下线；只要还有任一 active skill，工具在整个 turn 内保持可用。将来 skill 自带工具也能借此在 turn 开始时接入。

### 6.6 每轮重置选择

`AgentState.Skills` 与 `Activated` 都只在一轮内有效：`resetPerTurn` 清空它们，调用方每次 `Run` 都需要重新传选择。否则长生命周期的 agent 会让上一轮的选择永久生效。

### 6.7 Available 排序

`Available` 按名字排序，为的是确定性：同一组 skill 在任何一次调用里顺序一致，便于测试与排查。

## 七、请求与配置

### 请求

```json
{
  "text": "帮我填这份 PDF 表单",
  "model": "gpt-4.1",
  "metadata": {
    "skills": [{ "name": "pdf-processing" }]
  }
}
```

只有 `name` 被采用。请求中携带的 `installation`、`source`、`version` 等字段一律忽略，安装路径只来自服务端登记的 registry。否则调用方可以借此读取任意文件。

### 配置

skill 安装目录为 `<app_dir>/.agents/skills`（取自 `configs.Config.AppDir`，不做单独配置）。`AppDir` 为空、目录不存在或扫描失败时以无 skill 状态运行：不注册 `Skill` 工具。

## 八、错误处理与安全

| 场景                                             | 处理                                                  |
| ------------------------------------------------ | ----------------------------------------------------- |
| 请求的 skill 未安装                              | 开流前返回 HTTP 400，错误中给出名字                   |
| 选择的名字 registry 不认识（调用方绕过了预校验） | `Run` 返回错误，turn 以 runtime error 收场            |
| 显式选中的 skill 读取失败                        | 同上：`beforeUserInput` 直接失败，不做部分降级        |
| 模型调用不存在的 skill                           | 工具返回错误，并列出已安装的 skill 名，供模型自行纠正 |
| 扫描目录失败                                     | 记录告警，服务以无 skill 状态启动                     |
| 路径逃逸（`..`、符号路径越出 root）              | `ErrOutsideRoot`，拒绝读取                            |
| 正文超过 1 MiB                                   | `ErrManifestTooLarge`，拒绝读取                       |

路径校验依赖 `Loader` 记住的扫描根：只要 `Scan` 过的目录之外的路径都会被拒绝。没有扫描过任何目录时不做限制，这是为手动注册（例如测试）保留的行为。

## 九、预留与未实现

**已预留**

- `Skill` 结构可扩展 `Tools []string`（allowed-tools）。
- 扩展时，`SkillRegistry` 在 `Agent.init` 中把 skill 自带的工具注册进 `ToolRegistry`，激活条件写作 `slices.Contains(state.Skills, name) || slices.Contains(state.Activated, name)`。每 turn 计算 active set 正是为此准备的。

**未实现**

- 协议适配层无需 skill 专属渲染：两种披露路径到达适配层时都已是普通的 function call / function call output 消息对，由各协议适配层按普通工具调用渲染。
- lazy 路径的工具结果落库：`invokeToolCalls` 目前不调用 `saveMessages`，模型自主拉取的 tool result 暂不写 `messages` 表。
- `skill.activated` 事件：模型加载 skill 时尚未发出事件，trace 中只能通过工具调用看到。
- skill 自带脚本与工具执行。
- 数据库注册表：当前 skill 只来自本地目录。

## 十、测试

| 范围   | 文件                             | 覆盖                                                                                                 |
| ------ | -------------------------------- | ---------------------------------------------------------------------------------------------------- |
| Loader | `agent/skill/loader_test.go`     | frontmatter 解析、正文剥离、跳过无清单目录、路径逃逸、超限、编辑后缓存失效                           |
| 渲染   | `agent/llm/openai/skill_test.go` | 选择改写出的消息对渲染为 function_call / function_call_output 且 call_id 对齐、instructions 不被污染 |

运行：

```bash
go test ./agent/... -race
```

HTTP 层（gin、application 接线）目前只由编译保证，尚无自动化测试。
