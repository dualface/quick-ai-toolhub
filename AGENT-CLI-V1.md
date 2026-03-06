# AGENT-CLI-V1

本文件定义 `v1` 的 Agent CLI 运行方式。

## 结论

- `v1` 默认 runner 使用 `codex exec`
- `run-agent-tool` 额外支持 `claude --print` 和 `opencode run`
- `v1` 不选择 `codex` 的 HTTP / app server 方案

原因很简单：

- `codex exec` 是明确的非交互入口
- `codex exec` 原生支持 `--output-schema`
- `codex exec` 原生支持 `--json` 事件流和 `-o` 最终消息落盘
- `claude --print` 也支持结构化输出，但工作目录和最终消息落盘能力不如 `codex exec` 直接
- `opencode run` 可以做兼容支持，但 CLI 层没有看到原生 JSON schema 校验能力

## 支持的 runner

| runner_id      | CLI 入口           | 角色 |
| -------------- | ------------------ | ---- |
| `codex_exec`   | `codex exec`       | 默认 |
| `claude_print` | `claude --print`   | 兼容 |
| `opencode_run` | `opencode run`     | 兼容 |

## 能力对比

| runner_id      | 非交互执行 | 结构化输出 | 原生 schema 约束 | 会话续跑 | 工作目录控制 |
| -------------- | ---------- | ---------- | ---------------- | -------- | ------------ |
| `codex_exec`   | 是         | 是         | 是               | 是       | 是           |
| `claude_print` | 是         | 是         | 是               | 是       | 依赖进程 cwd |
| `opencode_run` | 是         | 是         | 否               | 是       | 是           |

说明：

- `codex_exec` 的结构化输出来自 `--output-schema`
- `claude_print` 的结构化输出来自 `--output-format json` + `--json-schema`
- `opencode_run` 的 `--format json` 是 JSON 事件输出，不等于原生 schema 校验

## 命令模板

### `codex_exec`

```bash
codex exec \
  --cd <worktree_path> \
  --output-schema <schema_file> \
  --json \
  -o <last_message_file> \
  -
```

补充：

- prompt 可以从 `stdin` 输入
- 需要续跑时使用 `codex exec resume`

### `claude_print`

```bash
claude \
  --print \
  --output-format json \
  --json-schema '<json_schema>' \
  --system-prompt '<system_prompt>' \
  '<prompt>'
```

补充：

- 命令应在目标 worktree 目录中执行
- 需要续跑时使用 `--continue`、`--resume` 或 `--session-id`

### `opencode_run`

```bash
opencode run \
  --dir <worktree_path> \
  --format json \
  --agent <agent_name> \
  '<prompt>'
```

补充：

- prompt 很长时，优先把上下文写入文件并通过 `-f/--file` 附加
- 需要续跑时使用 `--continue`、`--session` 或 `--fork`
- `run-agent-tool` 必须自行校验最终结果是否满足目标 schema

## `run-agent-tool` 实现要求

- 默认 runner 为 `codex_exec`
- 三种 runner 都在 task worktree 中执行
- 调用层统一传入同一份目标 schema
- `codex_exec` 和 `claude_print` 依赖 CLI 原生 schema 约束
- `opencode_run` 由 `run-agent-tool` 在 CLI 返回后做二次 JSON 解析和 schema 校验
- 任一 runner 返回非结构化结果时，统一映射为 `malformed_output`
- 不得直接继承用户本机默认的危险权限配置；调用时必须显式指定权限策略

## 权限策略

### 通用规则

- 所有 runner 都只允许在 task worktree 内工作
- 默认不允许使用任何“跳过权限检查”或“无沙箱”模式
- 只有目标 worktree 和显式声明的附加目录可以写入
- 不得直接在主仓库目录或用户 home 目录执行任务
- `run-agent-tool` 必须按 `agent_type` 映射权限，而不是交给调用方随意传入

### 角色到权限映射

| agent_type   | 默认权限 |
| ------------ | -------- |
| `developer`  | `workspace_write` |
| `qa`         | `workspace_write` |
| `reviewer`   | `read_only` |

说明：

- `qa` 仍使用 `workspace_write`，因为构建、测试和临时产物常会写入工作区
- `reviewer` 必须保持只读，不允许修改代码或工作区状态

### `codex_exec`

- `developer`、`qa` 必须显式传 `--sandbox workspace-write`
- `reviewer` 必须显式传 `--sandbox read-only`
- 禁止使用 `--dangerously-bypass-approvals-and-sandbox`
- 如需写入工作区外的产物目录，必须显式传 `--add-dir`
- 不依赖用户 `profile` 中的 sandbox 默认值

### `claude_print`

- 必须显式传 `--permission-mode dontAsk`
- 禁止使用 `--dangerously-skip-permissions`
- 禁止使用 `--allow-dangerously-skip-permissions`
- 必须显式传 `--allowed-tools`
- `reviewer` 不允许包含写入类工具
- 命令必须在目标 worktree 中执行；如需额外可写目录，显式传 `--add-dir`

建议的最小工具集：

- `developer`: `Bash,Read,Edit`
- `qa`: `Bash,Read`
- `reviewer`: `Read`

### `opencode_run`

- `opencode run` CLI 没有看到和 `codex` / `claude` 对等的显式权限参数
- 因此 `opencode_run` 只能作为兼容 runner，不作为最高信任 runner
- 必须显式传 `--dir <worktree_path>`
- 必须使用受控的 `--agent <agent_name>`，不能依赖用户默认 agent
- `reviewer` 对应的 agent 不得启用写入类工具
- `developer`、`qa` 对应的 agent 只允许最小必要工具集
- 不使用 `opencode attach`、`opencode serve` 或远程 server 模式执行 `v1` task

## 不纳入 `v1` 的内容

- `codex app-server`
- 自建 Agent HTTP 网关
- 不同 runner 之间的长会话迁移
