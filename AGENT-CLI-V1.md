# AGENT-CLI-V1

本文件定义 `v1` 的 Agent CLI 运行方式。

## 结论

- `v1` 只实现 `codex exec`
- `v1` 不选择 `codex` 的 HTTP / app server 方案

原因很简单：

- `codex exec` 是明确的非交互入口
- `codex exec` 原生支持 `--output-schema`
- `codex exec` 原生支持 `--json` 事件流和 `-o` 最终消息落盘

## 支持的 runner

| runner_id    | CLI 入口     | 角色 |
| ------------ | ------------ | ---- |
| `codex_exec` | `codex exec` | 默认 |

## 命令模板

### `codex_exec`

```bash
codex \
  --ask-for-approval never \
  --sandbox workspace-write \
  exec \
  --cd <worktree_path> \
  --output-schema <schema_file> \
  --json \
  -o <last_message_file> \
  -
```

补充：

- prompt 可以从 `stdin` 输入
- 需要续跑时使用 `codex exec resume`
- 默认模型和角色 prompt template 由 `config/config.yaml` + `prompts/agents/*.md` 提供
- 要允许 agent 任意读写当前工作目录，必须同时显式设置 `--cd <worktree_path>` 和 `--sandbox workspace-write`
- 只要 schema、最终消息文件或其他产物目录位于 worktree 外，就必须显式追加对应的 `--add-dir`
- `toolhub run-task --isolated-codex-home` 只作为 `developer` / `qa` 的后备隔离开关；默认仍保留用户现有 `HOME`/登录态

## `run-agent-tool` 实现要求

- runner 固定为 `codex_exec`
- 命令必须在 task worktree 中执行
- 角色 prompt 由固定骨架 prompt + `prompts/agents/<agent_type>.md` 模板共同组成
- CLI 未显式传 `--model` 时，默认模型从 `config/config.yaml` 读取
- 调用层统一传入同一份目标 schema
- 依赖 `codex exec` 原生 schema 约束
- 任一 runner 返回非结构化结果时，统一映射为 `malformed_output`
- 不得直接继承用户本机默认的危险权限配置；调用时必须显式指定权限策略

## 权限策略

### 通用规则

- Agent 只允许在 task worktree 内工作
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
- 仅在人工手动执行 `toolhub run-task --yolo` 时，允许改为传 `--dangerously-bypass-approvals-and-sandbox`，且此时不得再传 `--sandbox`
- 如需写入工作区外的产物目录，必须显式传 `--add-dir`
- 默认保留用户现有 `HOME`；只有 `developer` / `qa` 显式启用 `toolhub run-task --isolated-codex-home` 时，才把 `HOME` 重定向到 repo 内的 `.toolhub/runtime/home`
- 不依赖用户 `profile` 中的 sandbox 默认值

## 不纳入 `v1` 的内容

- `claude --print`
- `opencode run`
- `codex app-server`
- 自建 Agent HTTP 网关
