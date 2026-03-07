# SPRINTS-V1

本文件给出 `v1` 的实施拆解结果，后续可直接按此创建 `Sprint issue` 和 `Task sub-issue`。

## 约定

- 每个 `Sprint` 对应一个父 issue
- 每个 `Task` 对应该 `Sprint` 下的一个 sub-issue
- 同一 `Sprint` 下的 `Task` 默认串行执行
- 除非在创建 issue 前重排，否则编号一旦开始实施就不再变更

## Sprint 总览

| sprint_id   | 标题              | 目标                                                   |
| ----------- | ----------------- | ------------------------------------------------------ |
| `Sprint-01` | 项目基础骨架      | 建立可启动的单进程骨架、配置、数据库和命令执行基础设施 |
| `Sprint-02` | GitHub 任务投影   | 打通 GitHub issue / PR / CI 到本地 SQLite 投影         |
| `Sprint-03` | 调度与工作区准备  | 实现 Leader 选 task、建分支、建 worktree 的最小闭环    |
| `Sprint-04` | Task 执行闭环     | 实现单 task 的 Developer / QA / Reviewer 流程控制      |
| `Sprint-05` | Task PR 与 CI     | 打通 Task PR、GitHub Actions、自动合并和 issue 维护    |
| `Sprint-06` | Sprint 收尾与恢复 | 实现 Sprint PR、时间线日志、恢复与人工接管收口         |

## [Sprint-01] 项目基础骨架

### Goal

建立可启动、可配置、可连库、可执行外部命令的单进程基础骨架。

### Done When

- 二进制可以从 `config/config.yaml` 启动
- `SQLite` 可以基于 `sql/schema.sql` 初始化
- `task-state-store-tool` 的最小读写能力可用
- 进程日志统一走 `slog JSON`
- 可以统一执行 `gh` 和 `git` 并拿到结构化结果

### Tasks

| task_id   | 标题                           | 交付                                                      |
| --------- | ------------------------------ | --------------------------------------------------------- |
| `Task-01` | 初始化 Go 工程骨架             | 建立 `cmd/`、`internal/`、基础命令入口和最小启动流程      |
| `Task-02` | 实现 YAML 配置加载与校验       | 定义配置结构，加载 `config/config.yaml`，完成必要字段校验 |
| `Task-03` | 建立 SQLite/Bun store 基础设施 | 打开数据库、执行 schema 初始化、封装事务入口              |
| `Task-04` | 实现 task-state-store-tool     | 打通事件追加、状态投影读取更新和 outbox 入队的最小闭环    |
| `Task-05` | 建立统一日志与命令执行器       | 封装 `gh` / `git` / 其他外部命令调用和标准错误处理        |

## [Sprint-02] GitHub 任务投影

### Goal

将 GitHub 上的 `Sprint` / `Task` / 依赖 / PR / CI 信息投影到本地 `SQLite`，形成可调度输入。

### Done When

- 可以从 GitHub 全量读取 `Sprint issue`、`Task sub-issue` 和依赖关系
- 可以处理 webhook 增量更新并投影到本地
- 可以输出当前可调度的 `Sprint` / `Task` 列表
- 无效编号、孤立 task、跨 Sprint 依赖会被识别并记录

### Tasks

| task_id   | 标题                           | 交付                                                                |
| --------- | ------------------------------ | ------------------------------------------------------------------- |
| `Task-01` | 实现 GitHub CLI adapter        | 封装 `gh issue`、`gh pr`、`gh run`、`gh api` 的读取能力和数据归一化 |
| `Task-02` | 实现 github-sync-tool 全量对账 | 将 GitHub 的 issue / dependency / PR / CI 投影到 `SQLite`           |
| `Task-03` | 实现 github-sync-tool 增量同步 | 接入 webhook 事件并支持按 issue / PR / CI 定向对账                  |
| `Task-04` | 实现 get-task-list-tool        | 基于本地投影输出可调度的 `Sprint` / `Task` 列表和阻塞原因           |

## [Sprint-03] 调度与工作区准备

### Goal

让 `Global Leader` 能稳定选择下一个 task，并准备好分支和 worktree 作为执行环境。

### Done When

- `Global Leader` 可以选出下一个可执行 task
- `Sprint Branch` 和 `task branch` 可以按规范创建
- 对应 worktree 可以创建、复用和恢复
- task 启动前后的关键状态变化会落事件和状态投影

### Tasks

| task_id   | 标题                       | 交付                                                         |
| --------- | -------------------------- | ------------------------------------------------------------ |
| `Task-01` | 实现 Leader 调度选择逻辑   | 实现 `select-next-sprint`、`select-next-task` 和基础状态校验 |
| `Task-02` | 实现 git adapter           | 封装本地分支、提交、fetch、push、worktree 等 git 操作        |
| `Task-03` | 实现 prepare-worktree-tool | 创建或复用 `Sprint Branch`、`task branch` 和独立 worktree    |
| `Task-04` | 打通 Task 启动状态流       | 在 task 启动时写事件、更新投影并保存 worktree / branch 引用  |

## [Sprint-04] Task 执行闭环

### Goal

实现单个 task 内的 `Developer -> QA -> Reviewer` 闭环和失败回退逻辑。

### Done When

- `Task Orchestrator` 可以按阶段调用 `Developer`、`QA`、`Reviewer`
- 每个阶段结果都能以统一 schema 落到本地
- reviewer findings 可以聚合、去重并形成统一结论
- 重试次数、失败指纹、无进展判断开始生效

### Tasks

| task_id   | 标题                          | 交付                                             |
| --------- | ----------------------------- | ------------------------------------------------ |
| `Task-01` | 实现 run-agent-tool           | 建立 Agent 调用接口、超时控制和结果收集          |
| `Task-02` | 实现 Task Orchestrator 阶段机 | 打通 `developer`、`qa`、`review` 阶段推进和回退  |
| `Task-03` | 实现 review-aggregation-tool  | 实现 findings 聚合内核、去重、置信度提升和冲突识别 |
| `Task-04` | 接入 review 聚合与补充审查流  | 将 review-aggregation-tool 接入 orchestrator 并完成补充审查分支 |
| `Task-05` | 实现失败指纹与重试计数        | 生成 failure fingerprint 并维护各阶段失败计数 |
| `Task-06` | 实现无进展检测与升级建议      | 检测重复失败和长时间无进展并输出升级信号 |

## [Sprint-05] Task PR 与 CI

### Goal

将 task 内的代码变更接到 GitHub PR 和 GitHub Actions 上，形成单 task 的交付闭环。

### Done When

- `Task PR` 可以创建和更新
- `GitHub Actions` 状态可以映射为 task 决策输入
- 成功的 `Task PR` 可以自动合并到 `Sprint Branch`
- 失败时可以写 issue 评论并打 `needs-human`

### Tasks

| task_id   | 标题                        | 交付                                                       |
| --------- | --------------------------- | ---------------------------------------------------------- |
| `Task-01` | 实现 GitHub outbox worker   | 按 `outbox_actions` 执行幂等的 GitHub 写操作               |
| `Task-02` | 实现 task-pr-tool           | 创建 / 更新 `Task PR` 并回写本地 PR 投影                   |
| `Task-03` | 实现 ci-status-tool         | 读取 `GitHub Actions` 状态并判断 `ci_passed` / `ci_failed` |
| `Task-04` | 实现 issue-maintenance-tool | 追加评论、维护 `needs-human`、在完成时关闭 task issue      |

## [Sprint-06] Sprint 收尾与恢复

### Goal

完成 `Sprint` 级收尾、恢复和人工接管收口，使系统具备完整的 `v1` 运行闭环。

### Done When

- `Sprint PR` 创建前可以在 `Sprint Branch` 上并发执行 reviewer 审查并聚合结果
- `Sprint` 下所有 task 完成后可以创建 `Sprint PR`
- 每个 `Sprint` 都有连续可读的时间线日志
- 进程重启后可以恢复当前 `Sprint` / `Task` 状态
- 进入 `needs-human` 时能产出明确交接材料

### Tasks

| task_id   | 标题                           | 交付                                                                               |
| --------- | ------------------------------ | ---------------------------------------------------------------------------------- |
| `Task-01` | 实现 Sprint 级 reviewer 审查流 | 在 `Sprint Branch` 上并发启动 reviewer、聚合 findings，并形成 `Sprint PR` 前置结论 |
| `Task-02` | 实现 sprint-pr-tool            | 创建并跟踪 `Sprint PR`，等待人工审查与合并                                         |
| `Task-03` | 实现 timeline-log-tool         | 追加写 `logs/<sprint>.log` 并记录关键时间线事件                                    |
| `Task-04` | 实现 Leader 恢复与定时对账     | 启动恢复、周期性对账、补写缺失事件和状态修正                                       |
| `Task-05` | 实现人工接管收口               | 生成 handoff 摘要，统一 `needs-human` 触发和解除流程                               |

## 实施顺序说明

- 先完成 `Sprint-01` 到 `Sprint-03`，得到“能选 task、能建 worktree”的最小控制面
- 再完成 `Sprint-04` 到 `Sprint-05`，得到“能跑 task、能提 PR、能看 CI”的最小交付闭环
- 最后完成 `Sprint-06`，补齐 `Sprint PR`、恢复和人工接管
