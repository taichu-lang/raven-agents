# Sandbox 设计

> 本文描述 Agent Sandbox 抽象的设计：目标形态、关键设计问题的讨论结论，以及当前落地的最小集。总体架构见 [architecture.md](architecture.md)。

## 一、目标

Sandbox 是 agent 工具（首先是 `exec-command`，未来包括 python / mjs 代码解释器）的**隔离执行环境抽象**，一个 Sandbox 实例绑定一个 agent 会话（conversation）。

同一套接口要覆盖多种部署场景，由不同 `Provider` 实现：

| 场景 | 后端 |
| ---- | ---- |
| 本地/桌面 | 直接进程执行；CubeSandbox 封装 |
| 服务端 | docker 实例；kubernetes 实例组 |

调用方（工具层）只依赖 `sandbox` 包声明的接口，用 `Capabilities` 适配后端差异。依赖方向遵循项目规则：`sandbox` 是抽象层，各实现不反向依赖 agent。

## 二、主流 Agent 设计理念对比

### 流派 A：Shell-first（Claude Code / Codex / OpenCode / Cursor / Manus）

只有一个通用执行入口——shell 命令工具。**python / node 不是独立概念**，只是命令行的一部分：

| 产品 | 工具 | 关键字段 |
| ---- | ---- | -------- |
| Codex | `exec_command` | `cmd, shell, tty, login, workdir, yield_time_ms, sandbox_permissions` |
| OpenCode | `shell` | `command, workdir, timeout, background` |
| Claude Code | `Bash` | `command, timeout, run_in_background` |
| Manus | 单一 shell 工具 | "everything is a shell command" |

没有任何一家在 exec 工具上放 `language` / `shell-type` 字段（原因见 3.1）。

两个值得注意的例外：

- Codex 额外提供 `node_repl`（`js` / `js_reset` / `js_add_node_module_dir`）——有状态持久 JS REPL，但定位是辅助工具，不是主执行入口。
- OpenCode 的 `execute` 不是通用代码解释器，而是 "Code Mode"：在受限 JS runtime 里把 tools 当函数调用来编排，本质是工具调用的脚本化。

### 流派 B：Code-Interpreter-first（ChatGPT CI / Assistants API / Open Interpreter / E2B / Daytona）

面向"没有仓库、用户丢个 CSV 来画图"的通用助手场景：

- **ChatGPT Code Interpreter**：单 Python Jupyter kernel，有状态（变量跨 cell 保留）；没有 language 字段，跑 bash 得 `!command`，是隐式约定。
- **Open Interpreter / E2B / Daytona**：显式 `language` 字段（`python` / `javascript` / `bash` 枚举），由模型填。E2B/Daytona 作为 sandbox 基础设施，提供 `commands.run` + 分语言 runtime（python 走 Jupyter 协议、node 走 REPL、bash 走 pty）。
- 依赖靠**模板镜像预装**（E2B template、Daytona image），运行时补装靠 `pip install`。

### 结论

两派的分野是**场景**，不是技术优劣：

- 编码 agent（workspace 即仓库）→ shell-first 足够；
- 数据分析型助手（无仓库、artifact 交换频繁）→ 才需要有状态 code session；
- E2B/Daytona 这类基础设施层**两者都抽象**——本项目的 `sandbox` 包对标的正是这个位置。

## 三、关键问题与结论

### 3.1 exec-command 的"引导"识别：不需要 language 字段

exec-command 的设计目标是执行任意 bash 命令，但命令的引导（bootstrap）可能是 bash、python 或 node。**命令行字符串本身就是引导声明**：

```bash
python3 -m pytest tests/          # 引导是 python
node --input-type=module -e "..." # 引导是 node（mjs 语义）
bash -lc "..."                    # 引导是 bash
```

模型写命令时天然携带解释器信息；写错了（`command not found`）会看到 stderr 自我纠正。这就是主流 exec 工具都不设 `shell-type` 字段的原因——它是冗余信息。**Sandbox 抽象里 `Exec(cmd string)` 不需要 language 概念，语言多样性被 shell 吸收。**

唯一需要显式区分引导的地方是 **PTY/会话语义**：交互式会话里"这是 shell 还是 python REPL"影响输入输出的解释方式（REPL 要等 prompt、shell 可以靠 sentinel 标记命令结束）。这是会话层（Terminal / CodeSession）的事，不是一次性 Exec 的事。

### 3.2 skill.md 没有 shell-type；依赖准备分三层

Anthropic Agent Skills 规范的 frontmatter 只有 `name`、`description`、`license`、`allowed-tools`、`metadata`、`version`，**没有 shell-type**。skill 里的脚本（`scripts/*.py`）靠正文的自然语言指示让模型用 shell 执行（"scripts are executed, not loaded"）。依赖声明同样没有标准化：惯例是 skill 目录里放 `requirements.txt` / `package.json`，正文写明安装步骤，模型照做或失败后自纠。

**依赖准备的三层机制，模型只兜底最后一层**：

| 层 | 机制 | 谁负责 |
| -- | ---- | ------ |
| 镜像/模板层 | docker image / E2B template / CubeSandbox 环境预装通用栈（python+pandas/matplotlib、node+tsx） | Provider / `Spec.Image` |
| 会话初始化层 | sandbox 创建后跑 setup（skill 的 `requirements.txt`、`npm i`） | 框架，可由 skill frontmatter 驱动 |
| 运行时自纠层 | `ModuleNotFoundError` → 模型 `pip install` → retry | 模型，前提是 `Capabilities.Network` |

模型确实能识别依赖（代码是它写的），但**不应作为主要机制**：运行时安装慢、耗 token、要求放开网络。主流产品都把 80% 的依赖固化在镜像层。

**预留扩展**：`docs/skills.md` 第九节"skill 自带脚本与工具执行"未实现。将来若需要系统级预备依赖，可扩展 frontmatter（如 `runtime: python`、`dependencies: [...]`），skill 激活时由框架在 sandbox 里预装依赖、注册 skill 自带工具。这是"模型自纠"之外的确定性路径。

### 3.3 代码解释器：语言由模型给，走枚举字段

做独立的 code-interpreter 工具时，`language` 就是 schema 里的枚举字段，**由模型填**——模型正要写这段代码，当然知道是什么语言，这对它是零成本决策。所有显式流派（Open Interpreter、E2B、Assistants）都这么做。备选方案是按写入文件的扩展名推断（`.py` / `.mjs` → runtime），适合"先 write_file 再 run(path)"的产品形态，但多绕一步。

### 3.4 什么场景需要 Terminal（持续交互）

一次性 `Exec` 覆盖不了、必须要"活着的 PTY 会话 + 多轮输入"的场景很窄：

1. **交互式 REPL / CLI 程序（最硬的场景）**：`psql`、`sqlite3`、`redis-cli`、`python`、`gdb`、`delve`——程序驻留等待输入，状态（连接、事务、断点、变量）活在进程里。一次性 exec 只能每次冷启动（`psql -c`），跨命令状态丢失。
2. **依赖键盘交互的长驻前台进程**：dev server 快捷键协议（vite 按 `r` 重启）、jest watch、调试器附着（`pytest --pdb`）、交互式向导（`npm init`、`git rebase -i` 拉起 `$EDITOR`）。
3. **强 TTY 检测的程序**：非 TTY 下拒绝运行或行为退化。**注意这一条不需要会话**——一次性 exec 加 `TTY: true` 分配伪终端即可（`ExecToolInput.TTY` 正是这个用途）。
4. **shell 状态跨命令保留（伪需求）**：`cd` / `export` / `source activate` 的延续。主流做法（Claude Code 明确如此）是不依赖 shell 状态持久，用 `workdir` / `env` 参数显式传递。**不为此场景引入 Terminal。**

主流取舍：

| 产品 | 持续交互方案 |
| ---- | ------------ |
| Codex | 真 PTY 会话：`tty` + session id + `yield_time_ms`（等 10s 让出，返回 session 继续） |
| Claude Code | 无 PTY 交互；`run_in_background` + 输出轮询 + 可发送 stdin（轻量版） |
| OpenCode | 只有 `background` + 完成通知，不能向后台进程发输入 |

**结论**：对编码 agent 场景，场景 1/2 都有非交互替代（`-c` 参数、`--yes` 标志、后台 exec + ctx 取消），场景 3 用一次性 `TTY bool` 解决，场景 4 是伪需求——**最小集不含 Terminal**。后台执行也不进接口：工具层用 goroutine + ctx 取消即可在阻塞式 `Exec` 之上合成。

### 3.5 文件跨界：FileSystem API vs volume 挂载

两种不同的文件跨界机制，适用面不一样：

| | Workspace + Volume 挂载 | FileSystem API（显式传输） |
| - | ----------------------- | -------------------------- |
| 机制 | 宿主目录 bind mount 进沙箱（docker `-v`、k8s hostPath/PVC） | 跨边界拷贝：docker cp、k8s exec+tar、沙箱服务的 HTTP 文件端点 |
| 一致性 | 同一 inode，零拷贝，实时双向可见 | 传输语义，有拷贝成本，需显式触发 |
| 地域假设 | **要求沙箱与 agent 进程共享存储命名空间**：同机 docker、同节点 k8s 才行；远端沙箱（CubeSandbox、托管服务、跨节点 pod）没有可挂载的东西 | **后端无关**：本地、同机容器、远端服务统一一套接口 |
| 隔离/安全 | 在沙箱与宿主之间开洞：沙箱内进程可篡改挂载的宿主文件；多租户服务端通常不可接受 | 沙箱完全隔离，传输是受控、可审计的单点 |
| 运维成本 | 每会话 volume 生命周期、k8s 节点亲和性、清理泄漏卷 | 无状态，随 sandbox 生命周期走 |
| agent 侧代码 | 几乎为零（文件工具直接 `os` 包操作宿主路径） | 需要各后端实现 API |

**结论：两者不是二选一，是分层关系。** `FileSystem` 接口是契约（文件如何跨界的统一语义），volume 挂载是某些后端在契约之下的**实现优化**：

- local 后端：`ReadFile` = `os.ReadFile`（同一文件系统，天然零拷贝）；
- 同机 docker 后端：可以把 workspace volume 挂载进容器，`FileSystem` 直接操作宿主侧挂载点（对外同一接口，内部零拷贝）；或不挂载、用 docker cp 实现（强隔离）；
- k8s/远端后端：走 exec+tar 或沙箱服务的文件端点。

调用方完全不感知底下是挂载还是拷贝。反过来，若把"挂载/宿主路径"提升进 `Spec`（如暴露 `HostPath`），就把地域假设泄漏进抽象——只有一半后端能实现，工具层被迫按 Capability 分叉写两套逻辑。`Spec.WorkingDir` 的语义是"沙箱内的工作目录"，怎么 provision 由 Provider 自己决定。

## 四、全量目标设计

目标形态的完整接口（分期落地，见第五、六节）：

```go
// 生命周期
type Provider interface {
    Kind() string
    Create(ctx context.Context, spec *Spec) (Sandbox, error)
    Attach(ctx context.Context, id string) (Sandbox, error) // 重连持久沙箱（服务端进程重启后）
    Destroy(ctx context.Context, id string) error
    Close(ctx context.Context) error
}

type Spec struct {
    ID         string            // 建议用 conversation ID
    Image      string            // 依赖准备的主通道；local 忽略
    WorkingDir string            // 沙箱内工作目录；怎么 provision 是后端私事
    Env        map[string]string
    Limits     ResourceLimits    // MemoryBytes / CPUCores / DiskBytes / ProcessTimeout
}

// 会话级环境
type Sandbox interface {
    ID() string
    Exec(ctx context.Context, req *ExecRequest) (*ExecResult, error)                  // 一次性命令
    OpenTerminal(ctx context.Context, req *TerminalRequest) (Terminal, error)         // PTY 持续交互
    OpenCodeSession(ctx context.Context, req *CodeSessionRequest) (CodeSession, error) // 有状态解释器
    Files() FileSystem                                                                 // 文件跨界
    Capabilities() Capabilities
    Close(ctx context.Context) error
}

// 有状态解释器会话（python / mjs 代码解释器的底座）
type CodeSession interface {
    ID() string
    Language() Language          // "python" | "javascript"（Node.js，ESM/.mjs 语义）
    Run(ctx context.Context, req *RunRequest) (*RunResult, error) // imports/变量跨 Run 保留
    Close(ctx context.Context) error
}

// 交互式 PTY 会话
type Terminal interface {
    ID() string                  // 工具把 ID 返回给模型，后续调用继续交互
    Write(ctx context.Context, data []byte) error
    Resize(ctx context.Context, rows, cols int) error
    Output() <-chan []byte       // 会话结束时关闭
    Wait(ctx context.Context) (int, error)
    Close(ctx context.Context) error
}

// 文件跨界契约（挂载是其下的实现细节）
type FileSystem interface {
    ReadFile(ctx context.Context, path string) ([]byte, error)
    WriteFile(ctx context.Context, path string, data []byte) error
    List(ctx context.Context, path string) ([]FileEntry, error) // 空目录返回空切片，不返回 nil
    Remove(ctx context.Context, path string) error
    MkdirAll(ctx context.Context, path string) error
}
```

通用语义约定：

- **非零退出码 / 代码片段抛异常是 result，不是 error**：`ExecResult.ExitCode`、`RunResult.Error` 承载；Go error 只表示执行本身失败（后端不可达、超时机制失效等）。工具层把 result 编码进 tool result 给模型自纠。
- **流式输出**：请求携带 `Output chan<- OutputEvent`（`{Stream, Data}` 区分 stdout/stderr），实现不关闭该 channel；同时始终截断捕获进 result（`MaxOutputBytes` + `Truncated`）。
- **超时优先级**：请求 `Timeout` → `ResourceLimits.ProcessTimeout` → 后端默认；ctx deadline 永远生效。被杀时 `TimedOut = true`、`ExitCode = -1`。
- **TTY**：一次性 PTY（`ExecRequest.TTY`）解决 TTY 检测类程序，stdout/stderr 合并进 Stdout、可能含控制序列；多轮交互才用 Terminal。
- **会话重连**：`TerminalRequest.ID` / `CodeSessionRequest.ID` / `Provider.Attach` 支撑服务端沙箱在 agent 进程重启后续用（与 Agent 的 restore 机制对齐）。
- **并发安全**：agent 的工具调用是并发的（`invokeToolCalls`），Sandbox 实现必须并发安全。
- **哨兵错误**：`ErrNotFound`、`ErrNotSupported`，实现用 `%w` 包装，调用方 `errors.Is` 匹配。

### 工具面映射（agent 侧，后续实现参考）

```
exec-command   {cmd, shell?, workdir?, timeout?, tty?, background?, session_id?}   → {output, exit_code, session_id?}
send-input     {session_id, input}                                                 → {output}     # PTY 会话续交互
run-code       {language: python|javascript, code, session_id?}                    → {stdout, stderr, error?, session_id}
```

`run-code` 是否做成独立工具取决于产品定位：编码 agent 场景可以不做（exec 足够）；支持"无仓库的数据分析对话"时才值得上有状态 CodeSession。

## 五、最小集（当前实现）

`sandbox/sandbox.go` 当前只含 **Exec + 生命周期**：

| 已实现 | 说明 |
| ------ | ---- |
| `Provider` | `Kind` / `Create` / `Destroy` / `Close`（无 `Attach`） |
| `Spec` + `ResourceLimits` | `ID` / `Image` / `WorkingDir` / `Env` / `Limits` |
| `Sandbox` | `ID` / `Exec` / `Capabilities` / `Close` |
| `ExecRequest` / `ExecResult` | 含一次性 `TTY`、`Stdin`、流式 `Output`、`MaxOutputBytes` 截断、`TimedOut` |
| `Capabilities` | 只有 `Network`（决定能否运行时自纠装依赖） |
| `ErrNotFound` / `ErrNotSupported` | 哨兵错误 |

**刻意排除**（依据第三节的讨论）：

- `Terminal`：编码 agent 场景的持续交互需求都有非交互替代（3.4）；
- `CodeSession`：引导被 shell 命令行吸收（3.1），且产品定位未定（4 节末）；
- `FileSystem`：文件工具是否以沙箱为执行目标未定，挂载 vs API 的取舍随后端形态明确后再落（3.5）；
- `Provider.Attach`：随持久沙箱场景（服务端进程重启续用）一起加。

## 六、扩展路径

排除项将来都作为**新接口 + 新 Capabilities 位**加入，不破坏现有实现：

1. `FileSystem` + `Capabilities.FileSystem`：随文件工具上沙箱一起定；local/docker 后端可用挂载做零拷贝实现，远端后端走传输。
2. `Terminal` + `Capabilities.Terminal`：出现真实的 REPL/调试器交互需求时加；同时工具面补 `session_id` 语义与 `send-input` 工具。
3. `CodeSession` + `Capabilities.Languages`：确定支持数据分析型对话时加；`run-code` 工具按 `Capabilities.Languages` 决定是否注册（复用 `ToolActiveFunc` 机制）。
4. `Provider.Attach` + `Spec` 持久化语义：服务端沙箱池 / 进程重启续用时加。
5. skill frontmatter 扩展（`runtime` / `dependencies`）：随"skill 自带脚本与工具执行"（docs/skills.md 第九节）一起设计，落在会话初始化层（3.2）。

## 七、实现展望

- **第一个 Provider：本地直执行**（`sandbox/local`）：`os/exec` + 可选 `creack/pty`（TTY 请求）；`Image` 忽略，`Capabilities{Network: true}`；资源限制尽力而为（Linux 上可配合 rlimit）。
- **工具接线**：`NewExecTool` 改为闭包 `Sandbox` 实例（sandbox 每会话一个，工具每 agent 构建一次，闭包无状态，与 6.3 的工具无状态原则一致）；`ExecToolInput` 的 `tty` 字段直通 `ExecRequest.TTY`。
- **服务端 Provider**：docker（同机，可选 workspace 挂载）与 k8s 实例组（远端，`Destroy` 兼做泄漏回收），配置层按 `Kind()` 选择。
