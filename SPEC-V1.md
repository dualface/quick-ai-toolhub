# SPEC-V1

本文件用于承载 `v1` 的实施规范；`README.md` 保持为高层设计说明，不继续堆积细节规范。

## 范围

当前规格只固定以下问题：

- `v1` 的最稳分层
- `task-list` 的来源
- `Sprint` / `Task` 如何映射到 `GitHub Issues`
- `Global Leader` 如何从 `GitHub Issues` 读取下一个 `Sprint` 和下一个 `Task`
- `GitHub Issues` 与本地 `SQLite` 状态之间的职责边界

## V1 最稳分层

`v1` 采用三层分工，不将所有职责压到 `GitHub Issues` 上。

### Layer 1: GitHub Issues

用途：任务定义真源和人工协作入口。

负责：

- 定义 `Sprint` / `Task` 的层级
- 保存任务目标、验收条件、备注
- 保存显式 issue 依赖
- 承载人工讨论和人工决策痕迹

不负责：

- 保存精细运行时状态
- 保存完整事件序列
- 保存重试计数、失败指纹、幂等键

### Layer 2: SQLite

用途：本地控制面真源。

负责：

- 保存结构化事件
- 保存 `Task` / `Sprint` 当前运行时状态投影
- 保存 attempt、failure fingerprint、无进展判断输入
- 保存 worktree、branch、PR、CI、review 结果等执行元数据
- 支撑重启恢复、幂等去重和状态机推进

不负责：

- 作为人类编写任务定义的主入口
- 替代 GitHub 上的 issue 层级和讨论

### Layer 3: Sprint Timeline Log

用途：人类可读审计时间线。

负责：

- 记录每个 `Sprint` 的关键动作摘要
- 记录重要异常、升级、人工接管和最终结果
- 提供低门槛排查入口

不负责：

- 作为状态机真源
- 作为调度决策依据

### 分层原则

- `GitHub Issues` 是任务定义真源
- `SQLite` 是运行时控制真源
- `logs/<sprint>.log` 是人类可读时间线
- 任一自动化决策都必须以 `SQLite` 中的本地控制状态为准，而不是直接以 GitHub label 或评论推断
- 任一人工任务定义和层级关系都必须以 GitHub issue 结构为准，而不是仅写入本地数据库

### 为什么不采用 “只用 GitHub Issues”

`v1` 不采用 “GitHub Issues 同时承担任务定义和运行时控制” 的方案，原因如下：

- GitHub Issues 适合人类协作，不适合保存高频、细粒度、可恢复的控制状态
- 自动化系统需要结构化事件序列、幂等去重、attempt 计数和 failure fingerprint，这些不适合落到 labels 或 comments
- 仅依赖 GitHub 状态会让 webhook 重放、乱序回调和进程重启后的恢复逻辑变脆
- 将内部状态大量回写 GitHub 会污染 issue 界面，也会增加状态漂移风险

## V1 任务列表真源

`v1` 采用 `GitHub Issues` 作为任务列表的人类协作入口和任务定义真源。

具体约束如下：

- `Sprint` 使用一个父 issue 表示
- `Task` 使用该 `Sprint` issue 下的 sub-issue 表示
- `Global Leader` 只从当前仓库的 `GitHub Issues` 读取 `Sprint` 和 `Task`
- `SQLite` 不是任务定义真源；`SQLite` 只负责运行时控制、事件、重试和审计
- `v1` 不使用 `GitHub Projects`、`Milestones` 或 Markdown tasklist 作为调度真源

说明：

- GitHub 当前已原生支持 issue 层级和 sub-issues；虽然平台支持多级嵌套，但 `v1` 只允许一层 `Sprint -> Task`
- 如果后续需要更深层级，应在新版本规格中单独扩展，不在 `v1` 内隐式支持

## GitHub Issue 映射

### Sprint Issue

每个 `Sprint` 对应一个父 issue，必须满足：

- label: `kind/sprint`
- state: `open`
- 不得作为其他 issue 的 sub-issue
- 可以包含多个 `Task` sub-issues

标题格式：

```text
[Sprint-01] <summary>
```

示例：

```text
[Sprint-01] 初始化自动化研发闭环
```

正文模板：

```md
## Sprint ID

Sprint-01

## Goal

<一句话描述本 Sprint 的交付目标>

## Done When

- <条件 1>
- <条件 2>

## Notes

<可选>
```

### Task Issue

每个 `Task` 对应一个 `Sprint` 下的 sub-issue，必须满足：

- label: `kind/task`
- state: `open`
- 必须且只能属于一个父 `Sprint` issue
- `v1` 中不得再拥有自己的 sub-issues

标题格式：

```text
[Sprint-01][Task-01] <summary>
```

示例：

```text
[Sprint-01][Task-01] 建立 SQLite 事件存储
```

正文模板：

```md
## Sprint ID

Sprint-01

## Task ID

Task-01

## Goal

<一句话描述任务目标>

## Acceptance Criteria

- <条件 1>
- <条件 2>

## Out of Scope

- <非目标 1>

## Notes

<可选>
```

## 层级与依赖规则

### 层级规则

- `Sprint` 和 `Task` 的主从关系必须通过 GitHub 的 sub-issue 关系表达
- `Global Leader` 不解析 issue body 中的 Markdown 列表来推断层级
- `Task` 只有在被挂到对应 `Sprint` issue 下之后，才视为有效任务
- 孤立的 `kind/task` issue 视为无效输入，`Global Leader` 不得调度

### 依赖规则

- 同一 `Sprint` 下的 `Task` 默认按编号顺序串行执行
- 如果需要表达显式阻塞关系，使用 GitHub issue dependency 的 `blocked by` / `blocking`
- `Global Leader` 必须同时满足以下条件，才可启动某个 `Task`：
  - 该 `Task` 是所属 `Sprint` 中编号最小的未完成 task
  - 该 `Task` 的显式依赖已全部解除
- `v1` 不支持跨 `Sprint` 依赖；如发现跨 `Sprint` 依赖，必须报错并进入人工处理

## 编号与排序规范

为避免依赖 GitHub UI 排序，`v1` 的选择顺序只看业务编号，不看创建时间或最近更新时间。

约束如下：

- `Sprint` 编号格式固定为 `Sprint-XX`
- `Task` 编号格式固定为 `Task-XX`
- `Global Leader` 通过标题和正文中的编号提取业务顺序
- `Sprint` 的选择顺序按 `Sprint-XX` 数值升序
- 同一 `Sprint` 下 `Task` 的选择顺序按 `Task-XX` 数值升序
- 如果标题和正文中的编号不一致，视为无效输入

推荐的系统内标识：

- `sprint_id`: `Sprint-01`
- `task_local_id`: `Task-01`
- `task_id`: `Sprint-01/Task-01`

说明：

- GitHub issue number 只作为外部平台引用，不作为业务顺序依据
- `SQLite` 中必须同时保存业务编号和 GitHub issue number

### Issue 编号解析算法

`Global Leader` 按以下顺序解析 issue 编号：

1. 从 issue **标题**中提取：
   - Sprint issue 标题匹配正则 `^\[Sprint-(\d+)\]`
   - Task issue 标题匹配正则 `^\[Sprint-(\d+)\]\[Task-(\d+)\]`
2. 从 issue **正文**的对应 section 提取：
   - `## Sprint ID` 段落下的值，例如 `Sprint-01`
   - `## Task ID` 段落下的值，例如 `Task-01`（仅 task issue）
3. 对比两处提取结果：如果标题与正文的编号不一致，视为无效输入，停止调度并进入人工处理
4. 从编号中取数值部分生成 `sequence_no`：`Sprint-01` -> `1`，`Task-03` -> `3`

解析错误处理：

- 标题格式不匹配（未包含 `[Sprint-XX]`）：视为无效 issue，跳过并记录警告
- 正文缺少 `## Sprint ID` 或 `## Task ID` section：视为无效输入，进入人工处理
- 标题与正文编号不一致：视为无效输入，进入人工处理
- 同一 `Sprint` 下存在重复 `Task-XX` 编号：视为无效输入，进入人工处理

## 最小标签规范

`v1` 只使用少量标签，避免把运行时状态全部映射到 GitHub labels。

必需标签：

- `kind/sprint`
- `kind/task`

可选标签：

- `needs-human`

约束如下：

- `kind/sprint` 与 `kind/task` 互斥
- `needs-human` 只用于提示人类需要介入，不作为状态机真源
- `qa_failed`、`review_failed`、`ci_failed`、`ready_to_merge` 等运行时状态不得依赖 labels 表达

### `needs_human` 与 `awaiting_human` 的关系

`needs_human` 是本地 SQLite 字段（`tasks.needs_human` / `sprints.needs_human`）与 GitHub `needs-human` 标签的统一体现，规则如下：

- 进入 `awaiting_human`、`blocked`、`escalated` 时：必须同时设置本地 `needs_human=true`、写入 `human_reason`，并在对应 GitHub issue 上添加 `needs-human` 标签
- 从 `awaiting_human` 恢复到任意自动推进状态时：必须清除本地 `needs_human=false`，并移除 GitHub `needs-human` 标签
- 在 `dev_in_progress`、`qa_in_progress` 等中间态下，`needs_human` 必须为 `false`
- `needs_human=true` 不等价于 `awaiting_human`：`blocked` 和 `escalated` 也会设置 `needs_human=true`，但这些是终态，不能通过恢复事件自动继续

## GitHub 与 SQLite 的职责边界

职责划分如下：

- `GitHub Issues`
  - 保存 `Sprint` / `Task` 的层级关系
  - 保存人类编写的任务定义、验收条件和备注
  - 保存显式 issue 依赖关系
  - 作为人工讨论和最终查看入口
- `SQLite`
  - 保存事件日志
  - 保存 `Sprint` / `Task` 的运行时状态
  - 保存 attempt、failure fingerprint、PR 信息、CI 信息、worktree 信息
  - 作为 `Global Leader` 与 `Task Orchestrator` 的控制面真源

同步规则如下：

- GitHub issue 的标题、正文、层级和依赖变化，应同步更新到本地投影
- 本地运行时状态变化，不要求一比一回写到 GitHub label
- 发生需要人工处理的情况时，应在对应 issue 上追加评论，并可附加 `needs-human`
- `Task` 进入 `done` 后，由系统关闭对应 task issue
- `Sprint PR` 合并完成后，由系统关闭对应 sprint issue

## SQLite Schema

### 设计原则

- `SQLite` 只服务单仓库控制面，因此 schema 直接按单仓库设计，不为多租户预留复杂抽象
- 所有时间字段统一保存为 UTC ISO 8601 字符串
- 布尔值统一保存为 `INTEGER`，取值为 `0` 或 `1`
- 列表或结构化扩展字段统一保存为 JSON 字符串
- `events` 为追加写表，不允许更新和硬删除
- 业务实体表只保存当前投影，不保存完整历史；完整历史通过 `events` 回放
- 默认开启 SQLite foreign keys
- 除人工明确执行数据修复外，`v1` 不做级联删除

### 表清单

`v1` 固定以下表：

- `repo_config`
- `sprints`
- `tasks`
- `task_dependencies`
- `events`
- `review_findings`
- `pull_requests`
- `ci_runs`
- `outbox_actions`
- `sync_state`

### `repo_config`

单行表，用于固定当前仓库配置。

关键字段：

| 字段             | 类型      | 约束           | 说明                  |
| ---------------- | --------- | -------------- | --------------------- |
| `id`             | `INTEGER` | PK, 固定为 `1` | 单仓库固定单行        |
| `github_owner`   | `TEXT`    | NOT NULL       | 仓库 owner            |
| `github_repo`    | `TEXT`    | NOT NULL       | 仓库名                |
| `default_branch` | `TEXT`    | NOT NULL       | 主干分支，例如 `main` |
| `created_at`     | `TEXT`    | NOT NULL       | 创建时间              |
| `updated_at`     | `TEXT`    | NOT NULL       | 更新时间              |

### `sprints`

保存 `Sprint` 的当前投影。

关键字段：

| 字段                      | 类型      | 约束                 | 说明                                |
| ------------------------- | --------- | -------------------- | ----------------------------------- |
| `sprint_id`               | `TEXT`    | PK                   | 例如 `Sprint-01`                    |
| `sequence_no`             | `INTEGER` | NOT NULL, UNIQUE     | 从 `Sprint-XX` 提取出的顺序号       |
| `github_issue_number`     | `INTEGER` | NOT NULL, UNIQUE     | Sprint issue 编号                   |
| `github_issue_node_id`    | `TEXT`    | NOT NULL, UNIQUE     | GitHub 全局节点 ID                  |
| `title`                   | `TEXT`    | NOT NULL             | Issue 标题快照                      |
| `body_md`                 | `TEXT`    | NOT NULL             | Issue 正文快照                      |
| `goal`                    | `TEXT`    | NULL                 | 从正文解析出的 Goal                 |
| `done_when_json`          | `TEXT`    | NOT NULL             | `Done When` 列表                    |
| `status`                  | `TEXT`    | NOT NULL             | 对应 README 中的 Sprint 状态机      |
| `sprint_branch`           | `TEXT`    | NULL                 | 例如 `sprint/Sprint-01`             |
| `active_sprint_pr_number` | `INTEGER` | NULL                 | 当前 Sprint PR 编号                 |
| `timeline_log_path`       | `TEXT`    | NOT NULL             | 例如 `logs/Sprint-01.log`           |
| `needs_human`             | `INTEGER` | NOT NULL DEFAULT `0` | 是否需要人工介入                    |
| `human_reason`            | `TEXT`    | NULL                 | 最近一次需要人工介入的摘要          |
| `opened_at`               | `TEXT`    | NULL                 | GitHub issue 打开时间               |
| `closed_at`               | `TEXT`    | NULL                 | GitHub issue 关闭时间               |
| `last_issue_sync_at`      | `TEXT`    | NULL                 | 最近一次从 GitHub 同步 issue 的时间 |
| `created_at`              | `TEXT`    | NOT NULL             | 本地记录创建时间                    |
| `updated_at`              | `TEXT`    | NOT NULL             | 本地记录更新时间                    |

### `tasks`

保存 `Task` 的当前投影和运行时控制字段。

关键字段：

| 字段                          | 类型      | 约束                 | 说明                                |
| ----------------------------- | --------- | -------------------- | ----------------------------------- |
| `task_id`                     | `TEXT`    | PK                   | 例如 `Sprint-01/Task-01`            |
| `sprint_id`                   | `TEXT`    | NOT NULL, FK         | 关联 `sprints.sprint_id`            |
| `task_local_id`               | `TEXT`    | NOT NULL             | 例如 `Task-01`                      |
| `sequence_no`                 | `INTEGER` | NOT NULL             | 从 `Task-XX` 提取出的顺序号         |
| `github_issue_number`         | `INTEGER` | NOT NULL, UNIQUE     | Task issue 编号                     |
| `github_issue_node_id`        | `TEXT`    | NOT NULL, UNIQUE     | GitHub 全局节点 ID                  |
| `parent_github_issue_number`  | `INTEGER` | NOT NULL             | 父 Sprint issue 编号                |
| `title`                       | `TEXT`    | NOT NULL             | Issue 标题快照                      |
| `body_md`                     | `TEXT`    | NOT NULL             | Issue 正文快照                      |
| `goal`                        | `TEXT`    | NULL                 | 从正文解析出的 Goal                 |
| `acceptance_criteria_json`    | `TEXT`    | NOT NULL             | 验收条件列表                        |
| `out_of_scope_json`           | `TEXT`    | NOT NULL             | 非目标列表                          |
| `status`                      | `TEXT`    | NOT NULL             | 对应 README 中的 Task 状态机        |
| `attempt_total`               | `INTEGER` | NOT NULL DEFAULT `0` | 总修复循环次数；每次重新进入 `dev_in_progress` 进行一轮修复时加 1 |
| `qa_fail_count`               | `INTEGER` | NOT NULL DEFAULT `0` | QA 连续失败计数                     |
| `review_fail_count`           | `INTEGER` | NOT NULL DEFAULT `0` | Review 连续失败计数                 |
| `ci_fail_count`               | `INTEGER` | NOT NULL DEFAULT `0` | CI 连续失败计数                     |
| `current_failure_fingerprint` | `TEXT`    | NULL                 | 最近一次失败指纹                    |
| `active_pr_number`            | `INTEGER` | NULL                 | 当前 Task PR 编号                   |
| `task_branch`                 | `TEXT`    | NULL                 | 例如 `task/Sprint-01/Task-01`       |
| `worktree_path`               | `TEXT`    | NULL                 | 当前 worktree 目录                  |
| `needs_human`                 | `INTEGER` | NOT NULL DEFAULT `0` | 是否需要人工介入                    |
| `human_reason`                | `TEXT`    | NULL                 | 最近一次需要人工介入的摘要          |
| `opened_at`                   | `TEXT`    | NULL                 | GitHub issue 打开时间               |
| `closed_at`                   | `TEXT`    | NULL                 | GitHub issue 关闭时间               |
| `last_issue_sync_at`          | `TEXT`    | NULL                 | 最近一次从 GitHub 同步 issue 的时间 |
| `created_at`                  | `TEXT`    | NOT NULL             | 本地记录创建时间                    |
| `updated_at`                  | `TEXT`    | NOT NULL             | 本地记录更新时间                    |

附加约束：

- `UNIQUE (sprint_id, task_local_id)`
- `UNIQUE (sprint_id, sequence_no)`

### `task_dependencies`

保存显式 task 依赖。

关键字段：

| 字段                 | 类型   | 约束            | 说明                             |
| -------------------- | ------ | --------------- | -------------------------------- |
| `task_id`            | `TEXT` | PK 组成部分, FK | 被阻塞的 task                    |
| `depends_on_task_id` | `TEXT` | PK 组成部分, FK | 前置 task                        |
| `source`             | `TEXT` | NOT NULL        | 固定为 `github_issue_dependency` |
| `created_at`         | `TEXT` | NOT NULL        | 本地记录创建时间                 |

约束：

- 主键为 `(``task_id``, ``depends_on_task_id``)`
- 不允许自依赖
- 如果依赖目标不属于同一 `Sprint`，同步时直接报错并进入人工处理

### `events`

保存可回放的结构化事件；该表是本地控制面历史真源。

关键字段：

| 字段              | 类型      | 约束             | 说明                                                                     |
| ----------------- | --------- | ---------------- | ------------------------------------------------------------------------ |
| `event_id`        | `TEXT`    | PK               | 稳定事件 ID                                                              |
| `entity_type`     | `TEXT`    | NOT NULL         | `task` / `sprint` / `system`                                             |
| `entity_id`       | `TEXT`    | NOT NULL         | 对应实体 ID                                                              |
| `sprint_id`       | `TEXT`    | NULL             | 便于按 Sprint 查询                                                       |
| `task_id`         | `TEXT`    | NULL             | 便于按 Task 查询                                                         |
| `event_type`      | `TEXT`    | NOT NULL         | 例如 `task.qa_failed`                                                    |
| `source`          | `TEXT`    | NOT NULL         | `leader` / `orchestrator` / `github_webhook` / `reconciler` / `human` 等 |
| `attempt`         | `INTEGER` | NULL             | 关联的循环次数                                                           |
| `idempotency_key` | `TEXT`    | NOT NULL, UNIQUE | 幂等去重键                                                               |
| `payload_json`    | `TEXT`    | NOT NULL         | 事件正文                                                                 |
| `occurred_at`     | `TEXT`    | NOT NULL         | 事件发生时间                                                             |
| `recorded_at`     | `TEXT`    | NOT NULL         | 写入 SQLite 的时间                                                       |

约束：

- `events` 为 append-only 表，禁止更新和删除
- 同一外部副作用或 webhook 重放必须复用相同 `idempotency_key`

`idempotency_key` 生成规范：

- task 阶段事件：`<task_id>:<event_type>:<attempt>`，例如 `Sprint-01/Task-03:qa_failed:2`
- sprint 事件：`<sprint_id>:<event_type>`，例如 `Sprint-01:sprint_reviewing`
- outbox 动作：`outbox:<action_type>:<entity_id>:<attempt>`，例如 `outbox:create_pr:Sprint-01/Task-03:1`
- webhook 事件：`webhook:<delivery_id>`
- 不同来源（webhook、reconciler、leader）如果表达同一外部事实，必须产生相同的 `idempotency_key`

### `review_findings`

保存结构化 reviewer finding，便于单 reviewer 审查结果落地、后续修复追踪和人工复盘。

关键字段：

| 字段                  | 类型   | 约束                    | 说明                                                            |
| --------------------- | ------ | ----------------------- | --------------------------------------------------------------- |
| `finding_id`          | `TEXT` | PK                      | 本地 finding ID                                                 |
| `task_id`             | `TEXT` | NULL, FK                | 所属 task；Task 级 reviewer 必填，Sprint 级 reviewer 为 NULL    |
| `review_event_id`     | `TEXT` | NOT NULL, FK            | 来源事件，一般指向 `review_completed` 或 reviewer 原始结果事件 |
| `reviewer_id`         | `TEXT` | NOT NULL                | reviewer 会话标识                                               |
| `lens`                | `TEXT` | NOT NULL                | `correctness` / `test` / `architecture` / `security`            |
| `severity`            | `TEXT` | NOT NULL                | 严重级别；允许值：`critical` / `high` / `medium` / `low`       |
| `confidence`          | `TEXT` | NOT NULL                | 置信度；允许值：`high` / `medium` / `low`                      |
| `category`            | `TEXT` | NOT NULL                | 问题类别                                                        |
| `file_refs_json`      | `TEXT` | NOT NULL                | 文件引用列表                                                    |
| `summary`             | `TEXT` | NOT NULL                | finding 摘要                                                    |
| `evidence`            | `TEXT` | NOT NULL                | 证据                                                            |
| `finding_fingerprint` | `TEXT` | NOT NULL                | 去重指纹                                                        |
| `suggested_action`    | `TEXT` | NOT NULL                | 建议动作                                                        |
| `aggregate_status`    | `TEXT` | NOT NULL DEFAULT `open` | finding 处理状态；允许值：`open` / `accepted` / `dismissed` / `fixed`            |
| `created_at`          | `TEXT` | NOT NULL                | 写入时间                                                        |

建议唯一索引：

- `UNIQUE (review_event_id, reviewer_id, finding_fingerprint)`

约束补充：

- 每个 `review_event_id` 对应单个 reviewer 结果
- `finding_fingerprint` 在单个 `review_event_id` 内必须稳定唯一，供修复追踪和去重使用
- Task 级 reviewer：`task_id` 必填；Sprint 级 reviewer：`task_id` 为 NULL，通过 `review_event_id` 关联 sprint 事件

### `pull_requests`

保存 Task PR 和 Sprint PR 的当前投影。

关键字段：

| 字段                 | 类型      | 约束                 | 说明                         |
| -------------------- | --------- | -------------------- | ---------------------------- |
| `github_pr_number`   | `INTEGER` | PK                   | PR 编号                      |
| `github_pr_node_id`  | `TEXT`    | NOT NULL, UNIQUE     | GitHub 全局节点 ID           |
| `pr_kind`            | `TEXT`    | NOT NULL             | `task` / `sprint`            |
| `sprint_id`          | `TEXT`    | NOT NULL, FK         | 所属 Sprint                  |
| `task_id`            | `TEXT`    | NULL, FK             | Task PR 时必填               |
| `head_branch`        | `TEXT`    | NOT NULL             | 源分支                       |
| `base_branch`        | `TEXT`    | NOT NULL             | 目标分支                     |
| `status`             | `TEXT`    | NOT NULL             | `open` / `closed` / `merged` |
| `auto_merge_enabled` | `INTEGER` | NOT NULL DEFAULT `0` | 是否已启用自动合并           |
| `head_sha`           | `TEXT`    | NULL                 | 当前 HEAD SHA                |
| `url`                | `TEXT`    | NOT NULL             | PR 链接                      |
| `opened_at`          | `TEXT`    | NULL                 | 打开时间                     |
| `closed_at`          | `TEXT`    | NULL                 | 关闭时间                     |
| `merged_at`          | `TEXT`    | NULL                 | 合并时间                     |
| `last_synced_at`     | `TEXT`    | NULL                 | 最近一次与 GitHub 对账时间   |
| `created_at`         | `TEXT`    | NOT NULL             | 本地记录创建时间             |
| `updated_at`         | `TEXT`    | NOT NULL             | 本地记录更新时间             |

### `ci_runs`

保存 GitHub Actions 运行结果投影。

关键字段：

| 字段               | 类型      | 约束         | 说明                |
| ------------------ | --------- | ------------ | ------------------- |
| `github_run_id`    | `INTEGER` | PK           | workflow run ID     |
| `sprint_id`        | `TEXT`    | NOT NULL, FK | 所属 Sprint         |
| `task_id`          | `TEXT`    | NULL, FK     | Task 级 CI 时填写   |
| `github_pr_number` | `INTEGER` | NULL, FK     | 关联 PR             |
| `workflow_name`    | `TEXT`    | NULL         | Workflow 名称       |
| `head_sha`         | `TEXT`    | NULL         | 本次运行对应 SHA    |
| `status`           | `TEXT`    | NOT NULL     | GitHub Actions 状态 |
| `conclusion`       | `TEXT`    | NULL         | 成功 / 失败结论     |
| `html_url`         | `TEXT`    | NOT NULL     | 运行链接            |
| `started_at`       | `TEXT`    | NULL         | 开始时间            |
| `completed_at`     | `TEXT`    | NULL         | 完成时间            |
| `last_synced_at`   | `TEXT`    | NULL         | 最近一次同步时间    |
| `created_at`       | `TEXT`    | NOT NULL     | 本地记录创建时间    |
| `updated_at`       | `TEXT`    | NOT NULL     | 本地记录更新时间    |

### `outbox_actions`

保存待执行和已执行的 GitHub 外部副作用，保证幂等和可恢复。

关键字段：

| 字段                   | 类型      | 约束                 | 说明                                                                     |
| ---------------------- | --------- | -------------------- | ------------------------------------------------------------------------ |
| `action_id`            | `TEXT`    | PK                   | 本地动作 ID                                                              |
| `entity_type`          | `TEXT`    | NOT NULL             | `task` / `sprint` / `system`                                             |
| `entity_id`            | `TEXT`    | NOT NULL             | 对应实体 ID                                                              |
| `action_type`          | `TEXT`    | NOT NULL             | 允许值见下表                                                             |
| `github_target_type`   | `TEXT`    | NOT NULL             | `issue` / `pull_request` / `label`                                       |
| `github_target_number` | `INTEGER` | NULL                 | 目标 issue 或 PR 编号                                                    |
| `idempotency_key`      | `TEXT`    | NOT NULL, UNIQUE     | 外部动作幂等键                                                           |
| `request_payload_json` | `TEXT`    | NOT NULL             | 发往 GitHub 的参数                                                       |
| `status`               | `TEXT`    | NOT NULL             | `pending` / `running` / `succeeded` / `failed` / `canceled`              |
| `attempt_count`        | `INTEGER` | NOT NULL DEFAULT `0` | 动作重试次数                                                             |
| `last_error`           | `TEXT`    | NULL                 | 最近一次失败摘要                                                         |
| `next_attempt_at`      | `TEXT`    | NULL                 | 下一次重试时间                                                           |
| `created_at`           | `TEXT`    | NOT NULL             | 创建时间                                                                 |
| `updated_at`           | `TEXT`    | NOT NULL             | 更新时间                                                                 |
| `completed_at`         | `TEXT`    | NULL                 | 完成时间                                                                 |

`action_type` 允许值：

| action_type                   | 说明                             |
| ----------------------------- | -------------------------------- |
| `create_task_pr`              | 创建 Task PR                     |
| `update_task_pr`              | 更新 Task PR 标题或正文          |
| `enable_task_pr_auto_merge`   | 为 Task PR 启用自动合并          |
| `create_sprint_pr`            | 创建 Sprint PR                   |
| `comment_task_issue`          | 在 Task issue 追加评论           |
| `comment_sprint_issue`        | 在 Sprint issue 追加评论         |
| `set_needs_human`             | 为 issue 添加 `needs-human` 标签 |
| `clear_needs_human`           | 从 issue 移除 `needs-human` 标签 |
| `close_task_issue`            | 关闭 Task issue                  |
| `close_sprint_issue`          | 关闭 Sprint issue                |

### `sync_state`

保存全局同步游标和对账元数据。

关键字段：

| 字段         | 类型   | 约束     | 说明                          |
| ------------ | ------ | -------- | ----------------------------- |
| `name`       | `TEXT` | PK       | 例如 `last_full_reconcile_at` |
| `value_json` | `TEXT` | NOT NULL | JSON 值                       |
| `updated_at` | `TEXT` | NOT NULL | 更新时间                      |

### 最小索引要求

- `tasks(status, sprint_id, sequence_no)`
- `sprints(status, sequence_no)`
- `events(entity_type, entity_id, occurred_at)`
- `events(event_type, occurred_at)`
- `review_findings(task_id, finding_fingerprint)`
- `pull_requests(task_id, status)`
- `ci_runs(github_pr_number, status)`
- `outbox_actions(status, next_attempt_at)`

## GitHub 同步与投影规则

### 总体策略

- GitHub 是任务定义真源，`SQLite` 是运行时控制真源
- 同步采用 “启动全量对账 + Webhook 增量更新 + 定时对账” 三段式
- 任何从 GitHub 读到的定义类变更，必须先投影到 `SQLite`，再由 `Global Leader` 或 `Task Orchestrator` 消费
- 任何写往 GitHub 的外部动作，必须先写入 `outbox_actions`，再异步执行

### 入站同步

入站同步分为三类：

1. 启动对账
   - `Global Leader` 启动时先执行一次全量对账
   - 读取所有 `open` 的 `kind/sprint` issues 及其 sub-issues
   - 读取这些 issue 的依赖关系、标签、打开关闭状态
   - 将结果完整投影到 `sprints`、`tasks`、`task_dependencies`

2. Webhook 增量更新
   - GitHub 推送 issue、PR、CI 相关 webhook 后，先写 `events`
   - 再在单个 SQLite 事务中更新对应投影表
   - 如果 webhook 重复到达（相同 `delivery_id`），按 `idempotency_key` 静默去重，不重新执行投影更新
   - 如果 webhook 数据与本地投影存在实质冲突（例如 GitHub 返回的状态早于本地已记录状态），必须写入对账异常事件，不得静默覆盖；冲突事件由定时对账逻辑最终裁定

3. 定时对账
   - 每 `5` 分钟执行一次轻量全量对账
   - 目标是修复漏掉的 webhook、处理外部状态漂移、补齐缺失投影
   - 对账差异必须记录为结构化事件

### GitHub 到 SQLite 的投影范围

#### Sprint issue 投影

以下字段从 GitHub 投影到 `sprints`：

- issue number
- node id
- title
- body
- `open` / `closed` 状态
- `kind/sprint` 标签
- sub-issue 列表

#### Task issue 投影

以下字段从 GitHub 投影到 `tasks`：

- issue number
- node id
- title
- body
- `open` / `closed` 状态
- `kind/task` 标签
- 父 Sprint issue 关系
- issue dependencies

#### PR 与 CI 投影

以下字段从 GitHub 投影到 `pull_requests` 和 `ci_runs`：

- PR 打开、关闭、合并状态
- head/base branch
- auto-merge 开关状态
- workflow run 的状态和结论
- 与 PR 对应的 SHA 和链接

### SQLite 到 GitHub 的写回范围

`v1` 只回写少量高价值动作，不把本地所有运行时状态映射到 GitHub。

系统允许的写回动作如下：

- 为 task 创建 PR
- 为 task PR 开启自动合并
- 为 sprint 创建 PR
- 在 task issue 上追加关键评论
- 在 sprint issue 上追加关键评论
- 对需要人工介入的 issue 添加 `needs-human`
- 当人工问题解除后移除 `needs-human`
- task 完成后关闭 task issue
- sprint 完成后关闭 sprint issue

### 关键评论规则

系统只在以下时机自动写评论：

- task 开始执行时，可选写入“开始处理”评论
- task PR 创建成功后，写入 PR 链接
- task 进入 `awaiting_human`、`blocked` 或 `escalated` 时，写入阻塞摘要和建议动作
- task 完成并关闭 issue 前，写入完成摘要
- sprint PR 创建成功后，在 sprint issue 写入 PR 链接
- sprint 进入人工审查阶段时，在 sprint issue 写入审查提示
- sprint 完成并关闭 issue 前，写入 Sprint 完成摘要

### 冲突处理规则

以下规则固定为 `v1` 行为：

- 人类可随时修改 title、goal、acceptance criteria、notes；下次同步时直接覆盖本地定义投影
- 一旦 task 本地状态离开 `todo`，人类不得再修改其 `Sprint ID`、`Task ID`、父 Sprint 或排序编号
- 如果运行中的 task 被移动到其他 Sprint、改了编号或失去父 issue，系统必须停止自动推进，并将 task 标记为 `awaiting_human`
- 如果人类手动关闭了一个本地尚未 `done` 的 task issue，系统必须停止自动推进，并写入冲突事件
- 如果本地准备关闭某个 issue，而该 issue 已被人类提前关闭，则视为幂等成功
- 如果 GitHub 上的 issue、PR 或 workflow run 无法找到，而本地存在对应映射，系统必须写入对账异常并进入人工处理

### 读取前刷新规则

为降低脏读风险，以下动作前必须先执行一次针对性的 GitHub 刷新：

- `Global Leader` 选择下一个 `Sprint` 前
- `Global Leader` 选择下一个 `Task` 前
- `Task Orchestrator` 判断 PR 是否已合并前
- `Task Orchestrator` 判断 CI 是否已完成前
- 关闭任何 issue 前

### Outbox 执行规则

- 所有写 GitHub 的动作都先插入 `outbox_actions`
- worker 每次只领取 `pending` 或到达 `next_attempt_at` 的动作
- 执行成功后更新为 `succeeded`，并写入成功事件
- 遇到可重试错误时更新 `attempt_count`、`last_error`、`next_attempt_at`
- 遇到不可重试错误时更新为 `failed`，并由状态机决定是否进入 `awaiting_human`

### 最小 GitHub 交互面

`v1` 最小只实现以下交互能力：

- 读取 open sprint issues
- 读取 sprint sub-issues
- 读取 issue 详情、标签、状态、依赖
- 读取 PR 详情和合并状态
- 读取 GitHub Actions 运行状态
- 创建 Task PR
- 创建 Sprint PR
- 为 Task PR 启用自动合并
- 写 issue 评论
- 添加或移除 `needs-human`
- 关闭 issue

## Global Leader 的读取规则

`Global Leader` 在选择工作项时，必须按以下顺序执行：

1. 读取当前仓库中所有 `open` 的 `kind/sprint` issues
2. 按 `Sprint-XX` 升序排序
3. 选择第一个尚未完成且未被本地状态机判定为终态的 `Sprint`
4. 读取该 `Sprint` issue 的所有 sub-issues
5. 过滤掉非 `kind/task` issue 以及 PR 类型对象
6. 按 `Task-XX` 升序排序
7. 选择第一个满足以下条件的 `Task`
   - 对应 issue 仍为 `open`
   - 本地状态不是终态
   - 前序 task 均已完成
   - issue dependency 已满足

如果出现以下情况，必须停止自动推进并进入人工处理：

- 缺少 `kind/sprint` 或 `kind/task` 标签
- `Task` 没有父 `Sprint`
- `Task` 同时挂在多个父 issue 下
- `Task` 标题或正文缺少编号
- 同一 `Sprint` 下存在重复的 `Task-XX`
- 发现跨 `Sprint` 依赖

## GitHub API 使用约束

`v1` 的实现应尽量使用 GitHub 的原生 issue 层级和依赖接口，不自行解析非结构化文本来推断关系。

约束如下：

- 使用 GitHub sub-issues 能力维护 `Sprint -> Task` 层级
- 使用 GitHub issue dependencies 能力维护显式阻塞关系
- 使用 GitHub Issues API 读取 issue 基础字段、标签、状态、评论
- 不依赖 Markdown tasklist 解析来建立调度关系

## Sprint reviewer findings 处理规则

`Sprint` 进入 `sprint_reviewing` 后，`Global Leader` 必须按以下规则处理单个 reviewer 的结果：

1. 调用 `review-result-tool` 对 reviewer 结果执行 schema 校验、字段归一和决策收口，得到结构化 `decision`
2. 按 `decision` 决定后续状态：
   - `decision=pass`（无阻塞 finding）：直接进入 `Sprint PR` 创建流程，Sprint 状态转移至 `sprint_pr_open`
   - `decision=request_changes` 且 `has_critical_finding=true` 或 `has_blocking_finding=true`：Sprint 进入 `blocked`，附带 findings 摘要，禁止创建 `Sprint PR`
   - `decision=request_changes` 但无 `critical` / `blocking` finding：Sprint 进入 `awaiting_human`，由人工决定是否可以接受并继续
   - `decision=awaiting_human` 或 `has_reviewer_escalation=true`：Sprint 进入 `awaiting_human`，产出 handoff 摘要
3. Sprint reviewer 使用 `review-result-tool` 收口，与 task 级 reviewer 遵守完全相同的 `decision` 优先级规则
4. 审查结论必须以结构化摘要附加到 `Sprint PR` body 中（包括 `has_critical_finding`、`has_blocking_finding`、`has_reviewer_escalation` 三个结构化字段）
5. Sprint reviewer 默认使用 `architecture` lens，具体 lens 由 `Global Leader` 在调用 `run-agent-tool` 时显式传入
6. 当前 v1 中 `run-agent-tool` 和 `review-result-tool` 调用都必须显式传 `task_id`；不支持省略 `task_id`、仅凭 `sprint_id` 定位 reviewer 运行
7. reviewer findings 写入 `review_findings` 表时必须关联 `task_id`，并继续使用 `review_event_id` 标识对应 review 事件

## 审计与时间线

- `SQLite` 记录结构化事件
- 每个 `Sprint` 另外维护一个 `logs/<sprint>.log` 文件，记录人类可读时间线
- `Sprint` 时间线至少记录：
  - Sprint 初始化
  - Task 选择
  - Task 状态迁移摘要
  - 失败与升级
  - `Sprint PR` 创建
  - 人工审查 / 合并结果

## 官方参考

- GitHub Docs: [Adding sub-issues](https://docs.github.com/issues/managing-your-tasks-with-tasklists/creating-a-tasklist)
- GitHub Docs: [Creating issue dependencies](https://docs.github.com/en/issues/tracking-your-work-with-issues/using-issues/creating-issue-dependencies)
- GitHub Docs: [REST API endpoints for sub-issues](https://docs.github.com/en/rest/issues/sub-issues)
