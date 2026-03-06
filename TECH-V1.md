# TECH-V1

本文件只保留 `v1` 已确定的技术选项。

## 核心选型

- 主语言：`Go`
- 运行形态：单二进制、单进程、单仓库、单 `Global Leader`
- 数据库：`SQLite`
- 数据库访问：`Bun`
- 配置文件：统一使用 `YAML`
- 默认配置路径：`config/config.yaml`
- 数据库 schema 真源：[schema.sql](sql/schema.sql)
- GitHub 集成：统一使用 `gh`；常规操作走 `gh issue`、`gh pr`、`gh run`，缺少一等命令的能力走 `gh api`
- 本地 Git 操作：直接调用系统 `git` CLI
- Agent 执行：只使用 `codex exec`
- HTTP 服务：`net/http`
- 进程日志：`log/slog`，输出 `stdout`，格式为 `JSON`
- Sprint 时间线：`logs/<sprint>.log`

## 进程职责

`v1` 的单进程内包含：

- `Global Leader`
- GitHub Webhook 接收器
- 定时对账 worker
- outbox worker

`Task Orchestrator` 保持为逻辑角色，不要求拆成独立服务。

## 配置约束

- 所有主配置文件统一使用 `.yaml`
- 不维护“环境变量主配置 + YAML 辅助”的双轨模式
- 环境变量最多只用于指定配置文件路径，例如 `CONFIG_FILE`

## GitHub CLI 约束

- 所有 GitHub 出站操作统一通过 `gh` 执行
- 所有 `gh` 命令必须在目标仓库的 worktree 中执行
- 默认不要求执行 `gh repo set-default`
- 如果命令运行目录不在目标仓库 worktree 中，必须显式传 `-R <owner>/<repo>` 或先配置 `gh repo set-default`

## Agent CLI 约束

- `run-agent-tool` 固定使用 `codex_exec`
- `run-agent-tool` 必须在目标 task worktree 中执行
- `run-agent-tool` 必须显式设置权限策略，不能继承用户本机默认权限配置
- `developer`、`qa` 默认使用 `workspace_write`
- `reviewer` 默认使用 `read_only`
- `codex_exec` 使用 CLI 原生 schema 约束
- 详细命令约定见 `AGENT-CLI-V1.md`

## Tool I/O Schema

工具 I/O 统一使用 JSON-compatible object；配置文件仍使用 YAML。

### 通用约定

- 字段命名统一使用 `snake_case`
- 时间字段统一使用 UTC ISO 8601 字符串
- 所有 ID 使用 `SPEC-V1` 中已定义的业务 ID，例如 `Sprint-01`、`Sprint-01/Task-01`
- 带 `?` 的字段表示可选；未带 `?` 的字段表示必填
- 所有工具响应统一使用以下包裹结构：

```yaml
ok: true | false
data: object
error:
  code: string
  message: string
  retryable: true | false
```

约束：

- `ok: true` 时必须返回 `data`
- `ok: false` 时必须返回 `error`

### 共享结构

```yaml
artifact_refs:
  log: string
  worktree: string
  patch: string
  report: string

sprint_projection:
  sprint_id: string
  sequence_no: integer
  github_issue_number: integer
  status: string
  sprint_branch: string
  active_sprint_pr_number: integer
  needs_human: true | false
  human_reason: string

task_projection:
  task_id: string
  sprint_id: string
  task_local_id: string
  sequence_no: integer
  github_issue_number: integer
  status: string
  active_pr_number: integer
  task_branch: string
  worktree_path: string
  needs_human: true | false
  human_reason: string

finding:
  reviewer_id: string
  lens: string
  severity: string
  confidence: string
  category: string
  file_refs: [string]
  summary: string
  evidence: string
  finding_fingerprint: string
  suggested_action: string

pull_request_ref:
  github_pr_number: integer
  url: string
  status: string
  head_branch: string
  base_branch: string
  auto_merge_enabled: true | false

ci_run_ref:
  github_run_id: integer
  github_pr_number: integer
  status: string
  conclusion: string
  html_url: string
```

### `get-task-list-tool`

```yaml
request:
  refresh_mode: full | targeted
  sprint_id?: string

response.data:
  sprints: [sprint_projection]
  tasks:
    - task: task_projection
      blocked_by: [string]
  sync_summary:
    mode: full | targeted
    refreshed_at: string
    sprint_count: integer
    task_count: integer
  blocking_issues:
    - scope: repo | sprint | task
      entity_id: string
      reason: string
```

### `prepare-worktree-tool`

```yaml
request:
  sprint_id: string
  task_id: string
  sprint_branch: string
  task_branch: string
  worktree_root?: string

response.data:
  worktree_path: string
  task_branch: string
  base_branch: string
  base_commit_sha: string
  reused: true | false
```

### `task-state-store-tool`

```yaml
request:
  op: append_event | load_task_projection | load_sprint_projection | update_task_state | update_sprint_state | enqueue_outbox_action | load_pending_outbox_actions
  payload: object

payload.append_event:
  event_id: string
  entity_type: task | sprint | system
  entity_id: string
  sprint_id?: string
  task_id?: string
  event_type: string
  source: string
  attempt?: integer
  idempotency_key: string
  payload_json: object
  occurred_at: string

payload.load_task_projection:
  task_id: string

payload.load_sprint_projection:
  sprint_id: string

payload.update_task_state:
  task_id: string
  status: string
  attempt_total?: integer
  qa_fail_count?: integer
  review_fail_count?: integer
  ci_fail_count?: integer
  current_failure_fingerprint?: string
  active_pr_number?: integer
  task_branch?: string
  worktree_path?: string
  needs_human?: true | false
  human_reason?: string

payload.update_sprint_state:
  sprint_id: string
  status: string
  active_sprint_pr_number?: integer
  sprint_branch?: string
  needs_human?: true | false
  human_reason?: string

payload.enqueue_outbox_action:
  action_id: string
  entity_type: task | sprint | system
  entity_id: string
  action_type: string
  github_target_type: issue | pull_request | label
  github_target_number?: integer
  idempotency_key: string
  request_payload_json: object
  next_attempt_at?: string

payload.load_pending_outbox_actions:
  limit: integer

response.data:
  append_event:
    event_id: string
    deduplicated: true | false
  load_task_projection: task_projection
  load_sprint_projection: sprint_projection
  update_task_state:
    task: task_projection
  update_sprint_state:
    sprint: sprint_projection
  enqueue_outbox_action:
    action_id: string
    deduplicated: true | false
  load_pending_outbox_actions:
    actions: [object]
```

### `github-sync-tool`

```yaml
request:
  op: full_reconcile | ingest_webhook | reconcile_issue | reconcile_pull_request | reconcile_ci_run
  payload: object

payload.full_reconcile:
  reason: startup | periodic | manual

payload.ingest_webhook:
  delivery_id: string
  event_name: string
  payload_json: object

payload.reconcile_issue:
  github_issue_number: integer

payload.reconcile_pull_request:
  github_pr_number: integer

payload.reconcile_ci_run:
  github_run_id: integer

response.data:
  sync_summary:
    op: string
    started_at: string
    finished_at: string
    changed_count: integer
  changed_entities:
    - entity_type: sprint | task | pull_request | ci_run
      entity_id: string
```

### `run-agent-tool`

```yaml
request:
  agent_type: developer | qa | reviewer
  task_id: string
  attempt: integer
  lens?: string
  timeout_seconds?: integer
  context_refs:
    sprint_id: string
    worktree_path: string
    github_pr_number?: integer
    artifact_refs?: artifact_refs

response.data:
  runner: codex_exec
  status: string
  summary: string
  next_action: string
  failure_fingerprint?: string
  session_id?: string
  artifact_refs?: artifact_refs
  findings?: [finding]
```

### `review-aggregation-tool`

```yaml
request:
  task_id: string
  review_results:
    - reviewer_id: string
      lens: string
      status: string
      findings: [finding]

response.data:
  aggregated_findings: [finding]
  decision: pass | request_changes | awaiting_human
  summary: string
```

### `task-pr-tool`

```yaml
request:
  op: create_or_update_task_pr | get_task_pr | enable_auto_merge | refresh_merge_status
  payload: object

payload.create_or_update_task_pr:
  task_id: string
  sprint_id: string
  head_branch: string
  base_branch: string
  title: string
  body: string

payload.get_task_pr:
  task_id: string

payload.enable_auto_merge:
  github_pr_number: integer

payload.refresh_merge_status:
  github_pr_number: integer

response.data:
  pull_request: pull_request_ref
```

### `ci-status-tool`

```yaml
request:
  op: list_runs_for_pr | get_latest_required_status | sync_ci_projection
  payload: object

payload.list_runs_for_pr:
  github_pr_number: integer

payload.get_latest_required_status:
  github_pr_number: integer

payload.sync_ci_projection:
  github_pr_number: integer
  task_id: string
  sprint_id: string

response.data:
  runs: [ci_run_ref]
  overall_status: pending | success | failure
  overall_conclusion?: string
```

### `issue-maintenance-tool`

```yaml
request:
  op: comment_issue | set_needs_human | clear_needs_human | close_issue
  payload: object

payload.comment_issue:
  github_issue_number: integer
  body: string

payload.set_needs_human:
  github_issue_number: integer
  reason: string

payload.clear_needs_human:
  github_issue_number: integer

payload.close_issue:
  github_issue_number: integer

response.data:
  github_issue_number: integer
  comment_id?: integer
  needs_human?: true | false
  closed?: true | false
```

### `sprint-pr-tool`

```yaml
request:
  op: create_sprint_pr | get_sprint_pr | sync_sprint_pr_status
  payload: object

payload.create_sprint_pr:
  sprint_id: string
  head_branch: string
  base_branch: string
  title: string
  body: string

payload.get_sprint_pr:
  sprint_id: string

payload.sync_sprint_pr_status:
  sprint_id: string

response.data:
  pull_request: pull_request_ref
```

### `timeline-log-tool`

```yaml
request:
  op: append_log | append_state_change | append_error | append_human_handoff
  payload: object

payload.append_log:
  sprint_id: string
  line: string

payload.append_state_change:
  sprint_id: string
  entity_type: sprint | task
  entity_id: string
  from_status: string
  to_status: string
  summary: string

payload.append_error:
  sprint_id: string
  entity_type: sprint | task | system
  entity_id: string
  summary: string
  artifact_refs?: artifact_refs

payload.append_human_handoff:
  sprint_id: string
  entity_type: sprint | task
  entity_id: string
  reason: string
  suggested_action: string

response.data:
  log_path: string
  written: true | false
```

## 实现约束

- 不依赖 `Node.js`、`Python`、`JVM` 运行时
- 不要求容器才能运行
- 不依赖 `CGO` 作为必需运行前提
- 外部系统访问统一走 adapter 层
- GitHub adapter 统一封装 `gh` 调用，不直接内嵌 GitHub HTTP API client
- 数据库读写统一走 store 层
- store 层基于 `Bun` 实现
- 数据库结构仍以 [schema.sql](sql/schema.sql) 为真源，不以 ORM 自动建表为准

## 建议目录

```text
cmd/
  toolhub/
internal/
  leader/
  orchestrator/
  store/
  github/
  git/
  timeline/
sql/
  schema.sql
config/
  config.yaml
```

## 一句话结论

`v1` 使用 `Go + SQLite + gh + git CLI`，以单二进制长驻进程运行。
