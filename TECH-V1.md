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
- 不维护”环境变量主配置 + YAML 辅助”的双轨模式
- 环境变量最多只用于指定配置文件路径，例如 `CONFIG_FILE`

## 循环打断阈值配置

循环打断阈值在 `config/config.yaml` 中统一配置，作用域为全局所有 task：

```yaml
loop_breaker:
  max_total_attempts: 8          # 单个 task 总修复循环上限（含所有阶段）
  max_qa_fail_count: 3           # QA 连续失败上限
  max_review_fail_count: 2       # Review 连续打回上限
  max_ci_fail_count: 3           # CI 连续失败上限
  no_progress_min_diff_lines: 20 # 无进展判定：相邻两次提交净变更行数低于此值
```

无进展判定逻辑（**两个条件须同时满足**）：

1. 当前阶段的 `failure_fingerprint` 与上 N 次 attempt 相同（N = 对应阶段的 `max_*_fail_count`）
2. 相邻两次提交的净变更行数低于 `no_progress_min_diff_lines`，且未引入新文件

失败指纹连续重复但 diff 超阈值（代码改动大但方向不对）→ 不判定为无进展，仍继续直到命中分阶段上限。

约束：

- 所有阈值必须显式配置，不得缺省为 `0`（`0` 等同于禁用，禁止在生产配置中使用）
- `Task Orchestrator` 在每个阶段结束后读取当前阈值并执行判定
- 不支持 per-task 或 per-sprint 覆盖；`v1` 统一使用全局阈值

## GitHub CLI 约束

- 所有 GitHub 出站操作统一通过 `gh` 执行
- 所有 `gh` 命令必须在目标仓库的 worktree 中执行
- 默认不要求执行 `gh repo set-default`
- 如果命令运行目录不在目标仓库 worktree 中，必须显式传 `-R <owner>/<repo>` 或先配置 `gh repo set-default`

## Agent CLI 约束

- `run-agent-tool` 固定使用 `codex-cli`
- `run-agent-tool` 必须在目标 task worktree 中执行
- `run-agent-tool` 必须显式设置权限策略，不能继承用户本机默认权限配置
- `run-agent-tool` 默认从 `config/config.yaml` 读取 Agent 配置
- 不同 `agent_type` 的默认模型和 prompt template 由 `config/config.yaml` + `prompts/agents/*.md` 定义
- 模型优先级为：CLI 显式覆盖 > `agents.<agent_type>.model` > `default_model`
- 手工 bootstrap 时允许 `run-task --yolo`，该模式会向 `codex` 传 `--dangerously-bypass-approvals-and-sandbox`，并跳过 `--sandbox`
- `developer`、`qa` 默认使用 `workspace_write`
- `reviewer` 默认使用 `read_only`
- `codex-cli` 使用 CLI 原生 schema 约束
- 详细命令约定见 `AGENT-CLI-V1.md`

## Agent 运行产物目录规范

每次 Agent 运行创建一个唯一运行目录，规则如下：

- 运行目录统一落在仓库 worktree 内的 `.toolhub/runs` 根目录，格式为：
  `.toolhub/runs/<sprint_id>/<task_local_id>/<agent_type>/attempt-<nn>/<lens>/<timestamp>-<run_id>/`
  例如：`.toolhub/runs/Sprint-01/Task-03/developer/attempt-02/default/20260306T120000.000000000Z-runid123/`
- `developer` 和 `qa` 默认直接在上述目录下落盘
- `reviewer` 的持久化产物同样归档到上述目录；若底层 runner 为满足只读约束临时使用系统目录中转输出，最终 `runner.log` / `result.json` 仍回写到 `.toolhub/runs`
- 当前 v1 不定义“省略 `task_id` 的 sprint-reviewer 独立目录”；所有运行产物都按 `task_id` 归档
- 每个运行目录至少包含以下文件：
  - `prompt.md`：注入给 Agent 的完整提示
  - `output-schema.json`：期望的输出结构定义
  - `runner.log`：Agent 运行原始日志
  - `result.json`：Agent 结构化输出
- `artifact_refs.log` 指向该目录下的 `runner.log`
- `artifact_refs.report` 指向该目录下的 `result.json`

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
  reviewer_id: string       # 格式：<agent_type>-<lens>-<attempt>，例如 reviewer-correctness-2；Sprint 级：reviewer-architecture-1
  lens: string
  severity: string          # 允许值：critical | high | medium | low
  confidence: string        # 允许值：high | medium | low
  category: string
  file_refs: [string]
  summary: string
  evidence: string
  finding_fingerprint: string
  suggested_action: string  # reviewer 针对该 finding 的修复建议（自由文本，不做 enum 约束）

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

职责：创建或复用 Sprint Branch、task branch 和对应 worktree。

Sprint Branch 创建时机：`prepare-worktree-tool` 在收到请求时，若 `sprint_branch` 在远端不存在，则从 `default_branch` 的最新代码自动创建并推送；若已存在则直接使用。`Global Leader` 无需单独创建 Sprint Branch。

```yaml
request:
  sprint_id: string
  task_id: string
  sprint_branch: string   # 期望的 Sprint Branch 名，不存在时自动从 default_branch 创建
  task_branch: string
  worktree_root?: string

response.data:
  worktree_path: string
  task_branch: string
  base_branch: string
  base_commit_sha: string
  reused: true | false    # true 表示复用了已存在的 worktree，false 表示新建
```

### `task-state-store-tool`

```yaml
request:
  op: append_event | load_task_projection | load_sprint_projection | update_task_state | update_sprint_state | enqueue_outbox_action | load_pending_outbox_actions | append_review_findings
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

payload.append_review_findings:
  findings: [finding]
  review_event_id: string
  task_id: string      # 必填；当前 review findings 必须关联具体 Task
  sprint_id: string

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
  append_review_findings:
    stored_count: integer
    deduplicated_count: integer
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
  task_id: string         # 必填，格式为 Sprint-<n>/Task-<n>
  attempt: integer
  lens?: string
  model?: string
  config_file?: string
  timeout_seconds?: integer
  context_refs:
    sprint_id: string
    worktree_path: string
    github_pr_number?: integer
    artifact_refs?: artifact_refs

response.data:
  runner: codex-cli
  status: string
  summary: string
  next_action: string
  failure_fingerprint?: string
  session_id?: string
  artifact_refs?: artifact_refs
  findings?: [finding]
```

权限策略说明：

- `agent_type` 隐含权限策略：`developer` / `qa` 使用 `workspace_write`，`reviewer` 使用 `read_only`
- `run-agent-tool` 内部根据 `agent_type` 自动映射为 `codex` 的 `--sandbox` 参数，调用方不需要额外传权限字段
- `reviewer` 通过 `--add-dir <worktree_path>` 访问 worktree，读操作只读，不写入 worktree
- 当前 v1 不支持省略 `task_id`、仅凭 `sprint_id` 触发 reviewer 运行

### `review-result-tool`

```yaml
request:
  task_id: string     # 必填，格式为 Sprint-<n>/Task-<n>
  sprint_id?: string  # 可选上下文；未传时由 task_id 推断
  review_result:
    reviewer_id: string
    lens: string
    status: string
    findings: [finding]

response.data:
  review_findings: [finding]
  decision: pass | request_changes | awaiting_human
  summary: string
  has_critical_finding: true | false
  has_blocking_finding: true | false
  has_reviewer_escalation: true | false
```

职责边界：

- `review-result-tool` 负责对单个 reviewer 结果做 schema 校验、字段归一、枚举校验和决策优先级收口
- 调用方消费的是工具输出的稳定 contract，而不是原始 LLM 文本

标准 reviewer lens：

- `correctness`：默认 task reviewer lens；未命中特殊条件时使用
- `security`：当任务涉及安全边界、认证授权、密钥、依赖升级或外部输入处理时使用
- `architecture`：当任务主要修改公共接口、状态机、跨模块编排或存储 contract 时使用
- `test`：当任务主要补测试、修测试基础设施或验证策略时使用

单 reviewer 选择规则：

- task 级审查默认使用 `correctness`
- 若任务主要风险与上述更专门场景匹配，可切换为一个更合适的单 lens，但一次只能选择一个
- sprint 级审查默认使用 `architecture`

允许的 reviewer `status` 规范值：

- `pass`：review 通过，无 findings
- `request_changes`：review 要求修复，必须附带 findings
- `awaiting_human`：review 无法自动收口，要求人工裁决；可附带 findings

`review-result-tool` 到 orchestrator 事件的收口映射：

- `decision=pass` -> `review_passed`
- `decision=request_changes` -> `review_changes_requested`
- `decision=awaiting_human` -> `review_awaits_human`
- `ok=false` 且 `error.code=invalid_request` -> 视为当前 review attempt 失败，返回 `Developer` 修正输入/输出 contract
- 工具输出的 `decision` 是唯一 machine-readable 流程输入；orchestrator 不再直接解析 reviewer 原始 `status`

约束：

- `summary` 只做人类可读摘要；调用方不得解析 `summary` 文本代替结构化字段做流程分支
- `task_id` 必须符合 `Sprint-<n>/Task-<n>` 格式；当前 v1 不支持省略 `task_id` 的 review contract
- `review_result.reviewer_id` 必须非空
- `review_result.lens` 必须属于允许的 reviewer lens 枚举
- `review_result.status` 只允许 `pass`、`request_changes`、`awaiting_human`；其他值一律返回 `invalid_request`
- `request_changes` 必须携带至少一条 finding
- 每条 finding 必须保留非空 `file_refs`
- `has_critical_finding` / `has_blocking_finding` 的判定规则：
  - `has_critical_finding=true`：任一 finding 的 `severity=critical`
  - `has_blocking_finding=true`：任一 finding 的 `severity` 为 `critical` 或 `high`
  - 两者并非互斥：`critical` finding 同时触发两个 flag
- 最终 `decision` 与结构化 signals 的优先级必须满足：
  - 发现无效输入时，工具返回 `ok: false` / `error.code=invalid_request`
  - `has_reviewer_escalation=true` 时，`decision=awaiting_human`
  - 否则若 `has_critical_finding=true` 或 `has_blocking_finding=true`，`decision=request_changes`
  - 否则若存在普通 findings（`severity=medium` 或 `low`），`decision=request_changes`
  - 其他情况返回 `decision=pass`

findings 流转路径：

- `run-agent-tool` 从 reviewer 输出中提取 `findings: [finding]` 并原样返回给调用方
- 调用方将 `findings` 作为 `review_result.findings` 传入 `review-result-tool`
- `review-result-tool` 对每条 finding 做 schema 校验和枚举校验，返回归一后的 `review_findings: [finding]`
- 调用方（`Task Orchestrator` 或 `Global Leader`）将归一后的 findings 写入 `review_findings` 表（通过 `task-state-store-tool`）
- findings 写入时以 `(task_id, review_event_id, reviewer_id, finding_fingerprint)` 去重

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

三个操作的使用时机：

- `list_runs_for_pr`：调试或人工复盘时，列出 PR 关联的全部 CI run；一般不在自动推进主流程中轮询
- `get_latest_required_status`：主流程轮询使用，Task Orchestrator 等待 CI 完成时调用；返回合并所需的整体 CI 结论
- `sync_ci_projection`：在轮询前可调用一次，将 GitHub 最新 CI 状态同步写入本地 `ci_runs` 投影；主要由定时对账 worker 调用，也可在轮询前显式触发一次以降低脏读概率

Task PR 自动合并时序：

1. `review-result-tool` 返回 `decision=pass` 后，`Task Orchestrator` 调用 `task-pr-tool(create_or_update_task_pr)` 创建或更新 Task PR
2. PR 创建后，通过 `outbox_actions` 异步执行 `task-pr-tool(enable_auto_merge)`，启用 GitHub 自动合并
3. `Task Orchestrator` 可选先调用 `ci-status-tool(sync_ci_projection)` 刷新本地投影，再轮询 `ci-status-tool(get_latest_required_status)` 等待 CI 完成
4. CI 通过后，GitHub 平台根据已启用的自动合并规则自动执行合并
5. 合并完成由 webhook（`pull_request.closed` + `merged=true`）触发，写入 `auto_merge_succeeded` 事件
6. `Task Orchestrator` 通过 `task-pr-tool(refresh_merge_status)` 确认合并状态，再将 Task 标记为 `done`

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
  suggested_action: fix_and_resume | close_and_skip | manual_merge | investigate
  # fix_and_resume: 人工修复后继续自动推进
  # close_and_skip: 跳过该 task/sprint 不再自动推进
  # manual_merge:   人工处理合并相关外部条件
  # investigate:    人工调查后再决定下一步动作

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
