# 自动化 AI 研发团队

目标：构建一个完全自动化的 AI 研发团队。

## V1 实施边界

当前阶段先只实现一个最小可落地版本；以下约束在 `v1` 中视为固定前提，后续实现默认基于这些假设展开。

- 只支持单仓库模式：整个系统在任一时刻只服务一个 Git 仓库
- 仓库托管平台只支持 `GitHub`
- PR、Issue、Review、Branch、Webhook 等远程协作能力统一基于 `GitHub`
- CI 平台只支持 `GitHub Actions`
- 只运行单个 `Global Leader` 实例；`v1` 不实现多实例抢占、Leader 选举或分布式锁
- 人工参与范围只包括：
  - 准备和维护 `task-list`
  - 审查 `Sprint PR`
  - 按既有流程手动合并 `Sprint PR`
- 本地结构化状态存储优先使用 `SQLite`
- 每个 `Sprint` 必须维护一个独立的时间线日志文件，建议路径为 `logs/<sprint>.log`

由此带来的直接实现约束如下：

- 所有事件、状态投影、重试计数、失败指纹、运行元数据都优先落到本地 `SQLite`
- `SQLite` 是本地控制面的真实状态源；`.log` 文件用于保留便于人类阅读的 `Sprint` 级时间线
- `Sprint` 级 `.log` 文件记录的是该 `Sprint` 从初始化到结束的关键动作、状态迁移、异常、人工接管和最终结果
- `v1` 不要求支持跨仓库调度，不要求支持除 `GitHub Actions` 之外的 CI，不要求支持多个 `Global Leader` 协同

## 架构分层

该系统采用两层编排模型：

- `Global Leader`：负责全局调度，不负责记忆单个 task 的完整开发细节。
- `Task Orchestrator`：负责单个 task 的完整执行闭环。
- `Developer / QA / Reviewer`：在 `Task Orchestrator` 的任务上下文中工作。

## 任务层级

任务列表采用两级结构：

```text
Sprint-01
  +-- Task-01
  +-- Task-02
```

约束如下：

- `Sprint` 是交付和集成的最小批次
- `Task` 是开发和验证的最小执行单元
- 同一 `Sprint` 下的 `Task` 按顺序依次执行，而不是并行执行
- `Task Orchestrator` 始终只处理单个 `Task`
- `Global Leader` 负责同时维护 `Sprint` 级状态和 `Task` 级状态
- 只有当一个 `Sprint` 下的所有 `Task` 都达到完成条件后，才创建该 `Sprint` 的汇总 PR

### `Global Leader` 职责

- 读取 `PROJECT-DEVELOPER-GUIDE.md`
- 使用 `get-task-list-tool` 获取并筛选 `task-list`
- 选择下一个要执行的 `Sprint`，并按顺序选择该 `Sprint` 中的下一个 `Task`
- 调用 `prepare-worktree-tool` 为 task 创建独立的 Git worktree 和分支
- 启动该 task 对应的 `Task Orchestrator`
- 接收 `Task Orchestrator` 返回的结构化结果
- 更新 `Task` 和 `Sprint` 状态
- 当 `Sprint` 下所有 `Task` 完成后，创建该 `Sprint` 的汇总 PR
- 管理并发、重试上限、资源清理和全局通知

### `Task Orchestrator` 职责

- 只服务于单个 task
- 读取 `PROJECT-DEVELOPER-GUIDE.md`、所属 `Sprint` 上下文和 task
- 维护该 task 的阶段状态与执行日志
- 在 task 范围内启动 `Developer`、`QA`、`Reviewer`
- 作为 task 内唯一的流程控制器，根据 Agent 返回结果决定下一步
- 支持并发启动多个 `Reviewer` session，并对审查结果执行去重、聚合和分级
- 跟踪 PR、CI 和失败回退
- 维护重试次数、失败指纹和循环预算
- 必须在同一个 task 的 worktree、分支和 PR 上持续迭代，除非显式判定为需要废弃重建
- 将结果以摘要和引用的形式回传给 `Global Leader`

### Agent 职责

- `Developer`
  - 根据 task 开发功能并进行自测
  - 产出结构化结果，例如代码变更摘要、自测结果、待确认风险
  - 不直接启动 `QA`、`Reviewer` 或其他 Agent
- `QA`
  - 执行静态分析，包括 `lint`、`compile` 和 `build`
  - 执行测试
  - 产出结构化结果，例如通过/失败、问题摘要、日志引用
  - 不直接启动 `Reviewer` 或返回调度控制权之外的动作
- `Reviewer`
  - 评估代码质量和测试覆盖情况
  - 每个 `Reviewer` session 只负责一个明确审查视角，例如 `correctness`、`test`、`architecture`、`security`
  - 产出结构化结果，例如 `approve`、`request_changes`、遗留风险
  - 不直接创建 PR，也不直接启动其他 Agent

## Reviewer 并发与聚合策略

`Task Orchestrator` 必须支持在审查阶段并发运行多个 `Reviewer`，并对他们的 findings 做统一聚合。

强制规则如下：

- `Task PR` 的审查阶段默认启动 `1` 个 `Reviewer`
- 当 `Task` 涉及核心模块、安全权限、数据库迁移、公开接口、构建发布链路，或 diff 超过预设阈值时，`Task Orchestrator` 必须并发启动 `2` 到 `3` 个 `Reviewer`
- `Sprint PR` 创建前，`Global Leader` 必须在当前 `Sprint Branch` 上并发启动至少 `2` 个 `Reviewer`
- 并发 `Reviewer` 不能使用完全相同的审查视角，必须采用异质化配置
- 标准审查视角必须覆盖 `correctness`、`test`、`architecture`
- 当变更涉及安全边界、认证授权、密钥、依赖升级或外部输入处理时，必须额外加入 `security` 视角
- 每个 `Reviewer` 只返回结构化 findings，不直接返回流程控制动作

每条 finding 必须包含以下字段：

- `reviewer_id`
- `lens`
- `severity`
- `confidence`
- `category`
- `file_refs`
- `summary`
- `evidence`
- `finding_fingerprint`
- `suggested_action`

`Task Orchestrator` 或 `Global Leader` 在聚合 findings 时必须遵循以下规则：

- 按 `finding_fingerprint` 去重
- 同一问题被多个 `Reviewer` 命中时，必须提升其置信度和处理优先级
- 任一 `critical` finding 都必须阻塞当前流程并返回修复
- 单个 `Reviewer` 提出的中低置信度 finding 不得直接阻塞，必须先经过补充审查或二次验证
- 如果多个 `Reviewer` 结论冲突，必须发起补充审查，或将该问题升级为人工判定项
- 聚合后的审查结论必须以结构化摘要形式附加到 `Task PR` 或 `Sprint PR`
- 聚合结果中的 `summary` 只做人类可读摘要，不得作为唯一机器分支依据；调用方必须优先消费结构化字段
- 聚合结果至少必须结构化暴露：`has_critical_finding`、`has_blocking_finding`、`has_conflict`、`has_reviewer_escalation`、`needs_supplemental_review`
- 当 `blocking` 与 `conflict` 并存时，`conflict` 的人工/补充审查要求拥有更高的最终决策优先级，但 `blocking` 信号本身不得丢失
- 当只存在 `needs_supplemental_review` 且不存在 `blocking` / `conflict` / `reviewer_escalation` 时，聚合结果应保留非阻塞决策，同时显式返回补充审查信号供上层继续验证

## 上下文管理原则

为避免 `Global Leader` 的上下文窗口被多个 task 的历史细节撑爆，系统应遵循以下原则：

- `Global Leader` 只读取结构化摘要，不读取完整日志
- 单个 task 的完整上下文只保留在对应的 `Task Orchestrator` 中
- 每个阶段都输出统一结果对象，例如 `status`、`summary`、`artifact_refs`、`next_action`
- `Sprint` 级上下文只保留聚合摘要，例如 task 完成度、阻塞项、Sprint PR 状态
- 需要复盘时，再通过 `artifact_refs` 按需读取日志、补丁、测试报告或 PR 链接

示例结果对象：

```json
{
  "sprint_id": "Sprint-01",
  "task_id": "TASK-123",
  "stage": "qa",
  "status": "failed",
  "summary": "lint failed in src/api/router.ts",
  "attempt": 2,
  "failure_fingerprint": "eslint:src/api/router.ts:no-unused-vars",
  "artifact_refs": {
    "log": "logs/TASK-123/qa-02.log",
    "worktree": "worktrees/TASK-123"
  },
  "next_action": "return_to_developer"
}
```

## 事件驱动与恢复机制

为保证 `Global Leader` 和 `Task Orchestrator` 在进程重启、超时退出、Webhook 重放、外部系统短暂不一致等情况下仍能继续推进，系统必须采用事件驱动的状态落地方式。

强制约束如下：

- `Task` 和 `Sprint` 的真实状态来源必须是持久化事件日志，而不是仅保存在进程内存中的当前变量
- `Global Leader` 和 `Task Orchestrator` 的每一次状态变更都必须先写入事件，再更新派生状态视图
- 创建 worktree、创建或更新分支、创建 PR、写入评论、创建 Issue、触发自动合并等外部副作用，必须绑定稳定的 `idempotency_key`
- 收到重复的 Agent 结果、Webhook 回调或重试请求时，系统必须依据 `event_id` 或 `idempotency_key` 去重，禁止重复执行同一外部副作用
- `Global Leader` 和 `Task Orchestrator` 重启后，必须能够通过回放事件日志并结合 Git、PR、CI 的最新外部事实完成状态恢复
- 外部事件允许乱序到达；状态推进必须根据允许的状态转移规则、事件版本和实体当前状态决定是否接受该事件
- 所有派生状态都必须可重建，包括当前状态、重试计数、失败指纹、当前 worktree、活动 PR、最近一次人工接管动作

最小事件信封建议如下：

```json
{
  "event_id": "evt_TASK-123_qa_failed_02",
  "event_type": "task.qa_failed",
  "entity_type": "task",
  "entity_id": "TASK-123",
  "sprint_id": "Sprint-01",
  "attempt": 2,
  "caused_by": "qa",
  "idempotency_key": "TASK-123:qa:2",
  "occurred_at": "2026-03-06T10:00:00Z",
  "payload": {
    "failure_fingerprint": "eslint:src/api/router.ts:no-unused-vars",
    "artifact_refs": {
      "log": "logs/TASK-123/qa-02.log"
    }
  }
}
```

最小事件类型建议至少覆盖：

- `task_selected`
- `task_started`
- `developer_completed`
- `qa_passed`
- `qa_failed`
- `review_started`
- `review_aggregated`
- `task_pr_opened`
- `ci_started`
- `ci_passed`
- `ci_failed`
- `auto_merge_started`
- `auto_merge_succeeded`
- `auto_merge_failed`
- `task_escalated`
- `task_blocked`
- `sprint_review_started`
- `sprint_pr_opened`
- `sprint_done`

恢复规则如下：

- `Global Leader` 启动时必须先重建所有 `Sprint` 和 `Task` 的派生状态，再决定调度哪个 `Task`
- `Task Orchestrator` 启动时必须先重建该 `Task` 的当前状态、最近一次失败指纹、累计重试次数、活动 PR 和工作目录引用，再决定从哪个阶段继续
- 如果事件日志与外部系统状态冲突，必须先记录对账事件，再推进后续动作，禁止静默覆盖
- 对账后若发现外部副作用已经成功完成，但本地未记录成功事件，则必须补写对应事件，而不是重复执行外部动作

## 分支与 PR 策略

系统采用两层 PR 模型：

- `Sprint Branch`
  - 用途：作为该 `Sprint` 所有 `Task` 的集成基线
  - 命名规范：`sprint/<sprint>`
  - 生命周期：在 `Sprint` 开始时创建，在 `Sprint PR` 合并后结束
  - 规则：同一 `Sprint` 下的所有 `Task` 都从该分支的最新代码开始开发
- `Task PR`
  - 用途：交付单个 `Task` 的代码变更
  - 固定流向：`task/<sprint>/<task>` -> `sprint/<sprint>`
  - 触发条件：`Task Orchestrator` 完成 `Developer`、`QA`、`Reviewer` 和 CI 闭环
  - 合并策略：必须自动合并
  - 前提条件：评审通过、CI 通过、未命中循环打断规则
- `Sprint PR`
  - 用途：汇总一个完整 `Sprint` 的所有 task 结果
  - 固定流向：`sprint/<sprint>` -> `main` 或集成主干分支
  - 触发条件：同一 `Sprint` 下所有 `Task` 均已完成
  - 合并策略：禁止自动合并
  - 目标：保留一次更大范围的人工审查和最终集成确认

强制约束：

- `Sprint` 开始时先创建 `Sprint Branch`
- 同一 `Sprint` 内的 `Task` 按顺序依次执行
- 每个 `Task` 开始前，都从当前最新的 `Sprint Branch` 创建自己的 task 分支和 worktree
- 同一 `Sprint` 内的每个 `Task` 独立开发、独立验证、独立发起 `Task PR`
- `Task PR` 合并进入对应的 `Sprint` 分支，而不是直接进入主干
- 一个 `Task PR` 自动合并后，下一个 `Task` 必须基于更新后的 `Sprint Branch` 继续开发
- 只有 `Sprint` 内所有 `Task` 都完成后，才由 `Global Leader` 创建 `Sprint PR`
- `Sprint PR` 创建前，必须先对当前 `Sprint Branch` 执行多 `Reviewer` 并发审查，并附上聚合结论
- `Sprint PR` 必须由人工审查和人工合并
- 如果某个 `Task` 进入 `blocked` 或 `escalated`，则禁止生成该 `Sprint` 的汇总 PR

## ASCII Flow

```text
+------------------------------+
| Long-running system          |
| starts Global Leader         |
+--------------+---------------+
               |
               v
+------------------------------+
| Global Leader                |
| reads PROJECT-DEVELOPER-GUIDE.md|
+--------------+---------------+
               |
               v
         +-------------+
         | LOOP START  |
         +------+------+
                |
                v
+------------------------------+
| get-task-list-tool           |
| load sprint/task list        |
+--------------+---------------+
               |
               v
+------------------------------+
| select Sprint + next Task    |
+--------------+---------------+
               |
               v
+------------------------------+
| prepare-worktree-tool        |
| branch from latest           |
| sprint/<sprint>              |
+--------------+---------------+
               |
               v
+==========================================+
| Task Orchestrator (single task scope)    |
+----------------------+-------------------+
                       |
                       v
          +--------------------------+
          | Developer Agent          |
          | code + self-test         |
          +------------+-------------+
                       |
                       v
          +--------------------------+
          | result -> Orchestrator   |
          +------------+-------------+
                       |
                       v
          +--------------------------+
          | Orchestrator decides     |
          | next step                |
          +-----+---------------+----+
                | retry/fail     | qa
                v                v
          back to Dev      +------------------+
                           | QA Agent         |
                           | lint / build     |
                           | test             |
                           +-----+--------+---+
                                 | result
                                 v
                           +------------------+
                           | Orchestrator     |
                           | decides next step|
                           +-----+--------+---+
                                 | fail   | review
                                 v        v
                          back to   +------------------+
                          Developer | Reviewer Set     |
                                    | 1..N parallel    |
                                    +-----+--------+---+
                                          | findings
                                          v
                                    +------------------+
                                    | Orchestrator     |
                                    | aggregates +     |
                                    | decides next step|
                                    +-----+--------+---+
                                          | fail   | pass
                                          v        v
                                   back to   +------------------+
                                   Developer | Task PR + CI     |
                                             +-----+--------+---+
                                                   | fail   | pass
                                                   v        v
                                            create Issue   report result
                                            back to Dev    to Global Leader
+==========================================+
               |
               v
+------------------------------+
| Global Leader updates        |
| Task/Sprint status           |
+--------------+---------------+
               |
         +-----+------+
         | Sprint all |
         | tasks done?|
         +--+------+-+
            | no   | yes
            v      v
      +-------------+   +----------------------+
      | next Task   |   | create Sprint PR     |
      | from latest |   | no auto-merge        |
      | Sprint code |   +----------+-----------+
      +------+------+              |
             |                     v
             +------------> +----------------------+
                           | manual review/merge  |
                           +----------------------+
```

## 强制状态机

`Task` 必须维护以下状态：

- `todo`
- `in_progress`
- `dev_in_progress`
- `qa_in_progress`
- `qa_failed`
- `review_in_progress`
- `review_failed`
- `pr_open`
- `ci_in_progress`
- `ci_failed`
- `ready_to_merge`
- `merge_in_progress`
- `merge_failed`
- `awaiting_human`
- `done`
- `blocked`
- `escalated`
- `canceled`

说明：

- `in_progress` 是 task 被领取后的总入口状态，进入具体执行阶段后必须继续落到更细粒度的 `*_in_progress` 状态
- `Task PR` 的 `CI passed` 不应直接等于 `done`
- `Task PR` 自动合并完成后，必须将该 `Task` 标记为 `done`
- 如果 `Task PR` 尚未完成自动合并，则 `Task Orchestrator` 必须先回报 `ready_to_merge`
- `awaiting_human` 表示系统已经停止自动推进，并给出了明确的人类处理动作，恢复后必须显式回到某个允许的自动状态
- `merge_failed` 表示 PR 已满足合并前条件，但在自动合并动作本身失败，例如分支保护、竞态更新或平台错误
- `done`、`blocked`、`escalated`、`canceled` 为终态；终态只能通过人工创建新的 task 或显式重开流程来继续
- 同一 task 的多轮修复必须复用原有 worktree、分支和 PR
- 同一 `Sprint` 下的后续 `Task` 必须基于前序 `Task PR` 合并后的最新 `Sprint Branch`
- 当达到循环上限或重复命中同类失败时，task 必须进入 `escalated` 或 `blocked`

`Sprint` 必须维护以下状态：

- `todo`
- `in_progress`
- `partially_done`
- `ready_for_sprint_pr`
- `awaiting_human`
- `sprint_pr_open`
- `sprint_reviewing`
- `merge_failed`
- `done`
- `blocked`
- `canceled`

说明：

- 当 `Sprint` 下部分 `Task` 完成、部分仍在进行中时，必须标记为 `partially_done`
- 当 `Sprint` 下所有 `Task` 完成后，`Sprint` 必须进入 `ready_for_sprint_pr`
- `Sprint PR` 创建后必须进入 `sprint_pr_open`
- `awaiting_human` 表示 `Sprint PR` 已经创建或评审已结束，但需要等待人工审查、人工确认或人工合并
- `merge_failed` 表示 `Sprint PR` 已获批准，但在最终合并动作上失败
- `Sprint PR` 不允许自动合并，必须经过人工审查和人工合并
- `Sprint PR` 合并完成后，必须将 `Sprint` 标记为 `done`
- `done`、`blocked`、`canceled` 为终态

### `Task` 状态转移规则

下表定义 `Task` 的主要允许转移；未在表中定义的跨状态跳转一律视为非法。

| 当前状态             | 触发事件 / 条件                      | 下一状态                 | 说明                                                  |
| -------------------- | ------------------------------------ | ------------------------ | ----------------------------------------------------- |
| `todo`               | `task_started`                       | `in_progress`            | `Global Leader` 已选择该 task，并完成执行上下文初始化 |
| `in_progress`        | `developer_started`                  | `dev_in_progress`        | `Task Orchestrator` 启动 `Developer`                  |
| `dev_in_progress`    | `developer_completed` 且自测通过     | `qa_in_progress`         | 进入 `QA` 阶段                                        |
| `dev_in_progress`    | `developer_blocked`                  | `blocked`                | 开发阶段确认无法继续自动推进                          |
| `qa_in_progress`     | `qa_passed`                          | `review_in_progress`     | 进入审查阶段                                          |
| `qa_in_progress`     | `qa_failed`                          | `qa_failed`              | 记录失败指纹与日志引用                                |
| `qa_failed`          | `retry_approved` 且未命中循环打断    | `dev_in_progress`        | 返回开发修复                                          |
| `qa_failed`          | 命中循环上限、重复失败或无进展       | `escalated` 或 `blocked` | 由 `Task Orchestrator` 判定                           |
| `review_in_progress` | `review_aggregated` 且结论为通过     | `pr_open`                | 创建或更新 `Task PR`                                  |
| `review_in_progress` | `review_aggregated` 且结论为退回     | `review_failed`          | 记录聚合 findings                                     |
| `review_failed`      | `retry_approved` 且未命中循环打断    | `dev_in_progress`        | 返回开发修复                                          |
| `review_failed`      | finding 冲突、置信度不足或需人工裁决 | `awaiting_human`         | 停止自动推进，等待人工判定                            |
| `review_failed`      | 命中循环上限、重复失败或无进展       | `escalated` 或 `blocked` | 由 `Task Orchestrator` 判定                           |
| `pr_open`            | `ci_started`                         | `ci_in_progress`         | PR 已创建且 CI 已触发                                 |
| `ci_in_progress`     | `ci_passed`                          | `ready_to_merge`         | 可以进入自动合并                                      |
| `ci_in_progress`     | `ci_failed`                          | `ci_failed`              | 记录失败指纹与日志引用                                |
| `ci_failed`          | `retry_approved` 且未命中循环打断    | `dev_in_progress`        | 返回开发修复                                          |
| `ci_failed`          | 需要人工处理平台问题或权限问题       | `awaiting_human`         | 例如 CI 平台异常、权限配置缺失                        |
| `ci_failed`          | 命中循环上限、重复失败或无进展       | `escalated` 或 `blocked` | 由 `Task Orchestrator` 判定                           |
| `ready_to_merge`     | `auto_merge_started`                 | `merge_in_progress`      | 进入自动合并动作                                      |
| `merge_in_progress`  | `auto_merge_succeeded`               | `done`                   | `Task PR` 已自动合并进入 `Sprint Branch`              |
| `merge_in_progress`  | `auto_merge_failed`                  | `merge_failed`           | 保留失败原因和平台返回信息                            |
| `merge_failed`       | `merge_retry_approved`               | `merge_in_progress`      | 对同一 PR 重试自动合并                                |
| `merge_failed`       | 需要人工处理分支保护、冲突或平台异常 | `awaiting_human`         | 不得静默重复尝试                                      |
| `awaiting_human`     | `human_resume_to_dev`                | `dev_in_progress`        | 人工给出明确修复方向后恢复自动推进                    |
| `awaiting_human`     | `human_resume_to_merge`              | `merge_in_progress`      | 人工处理外部条件后恢复合并                            |
| 任意非终态           | `task_canceled`                      | `canceled`               | 仅允许由 `Global Leader` 或人工操作触发               |
| 任意非终态           | `task_blocked`                       | `blocked`                | 形成明确阻塞项并停止推进                              |
| 任意非终态           | `task_escalated`                     | `escalated`              | 需要更高优先级的人类介入                              |

补充约束：

- 同一状态转移必须附带唯一事件，且事件处理必须满足幂等
- `qa_failed`、`review_failed`、`ci_failed` 再次进入相同状态时，必须更新 `attempt`、`failure_fingerprint` 和引用日志，而不是丢弃该轮失败
- `pr_open`、`ci_in_progress`、`merge_in_progress` 之间的推进必须以外部系统事实为准，禁止仅凭本地推断直接跳转

### `Sprint` 状态转移规则

下表定义 `Sprint` 的主要允许转移；未在表中定义的跨状态跳转一律视为非法。

| 当前状态                          | 触发事件 / 条件                           | 下一状态                      | 说明                                         |
| --------------------------------- | ----------------------------------------- | ----------------------------- | -------------------------------------------- |
| `todo`                            | `sprint_initialized`                      | `in_progress`                 | 创建 `Sprint Branch` 并准备开始第一个 `Task` |
| `in_progress`                     | 首个 `Task` 完成且仍有后续 `Task`         | `partially_done`              | 进入“部分完成”状态                           |
| `in_progress`                     | 当前 `Sprint` 下唯一 `Task` 完成          | `ready_for_sprint_pr`         | 直接进入 `Sprint PR` 准备阶段                |
| `partially_done`                  | 仍有未完成 `Task` 被持续推进              | `partially_done`              | 自循环合法，但必须伴随新的 task 事件         |
| `in_progress` 或 `partially_done` | 任一 `Task` 进入 `blocked` 或 `escalated` | `blocked`                     | 当前 `Sprint` 不得创建汇总 PR                |
| `partially_done`                  | 所有 `Task` 均为 `done`                   | `ready_for_sprint_pr`         | 可以启动 `Sprint` 级审查                     |
| `ready_for_sprint_pr`             | `sprint_review_started`                   | `sprint_reviewing`            | 启动并发 `Reviewer` 审查                     |
| `sprint_reviewing`                | 聚合结论要求修复或人工裁决                | `blocked` 或 `awaiting_human` | 按问题性质决定是否允许恢复                   |
| `sprint_reviewing`                | 审查通过且 `Sprint PR` 已创建             | `sprint_pr_open`              | 进入人工审查 / 合并阶段                      |
| `sprint_pr_open`                  | 等待人工审查或人工合并                    | `awaiting_human`              | 系统不再自动推进                             |
| `awaiting_human`                  | `sprint_pr_merged`                        | `done`                        | 人工审查和合并已完成                         |
| `awaiting_human`                  | `sprint_merge_failed`                     | `merge_failed`                | 平台拒绝合并或发生最终集成冲突               |
| `merge_failed`                    | `human_retry_merge`                       | `awaiting_human`              | 人工修复后重新进入等待合并                   |
| 任意非终态                        | `sprint_canceled`                         | `canceled`                    | 仅允许由 `Global Leader` 或人工操作触发      |

## 循环打断机制

为避免 task 在 `Developer -> QA -> Reviewer -> CI` 之间无限循环，`Task Orchestrator` 必须维护以下机制：

- 总迭代上限：必须配置，例如单个 task 的上限为 5 到 8 轮完整修复循环
- 分阶段重试上限：必须配置，例如 `QA` 连续失败 3 次、`Reviewer` 连续打回 2 次、CI 连续失败 3 次即停止自动推进
- 失败指纹去重：如果连续多轮命中同一个错误指纹，例如同一文件同一类 lint 失败，必须直接升级而不是继续重复尝试
- 无进展检测：如果多轮提交的 diff 很小，且失败原因未变化，必须判定为无实质进展
- 升级策略：达到上限后，必须将 task 标记为 `escalated` 或 `blocked`，并附带最近几轮摘要、日志引用和明确的人类处理动作

阈值必须由 `Global Leader` 统一配置，并由 `Task Orchestrator` 在单个 task 内执行判定。

## 核心流程

1. 持续运行的系统启动 `Global Leader`
2. `Global Leader` 读取 `PROJECT-DEVELOPER-GUIDE.md`
3. `Global Leader` 使用 `get-task-list-tool` 获取 `task-list`
4. `Global Leader` 选择一个 `Sprint`，并按顺序挑选该 `Sprint` 中下一个未完成的 `Task`
5. 如果该 `Sprint` 尚未初始化，则先创建对应的 `Sprint Branch`
6. `Global Leader` 调用 `prepare-worktree-tool`，从该 `Sprint Branch` 的最新代码为当前 `Task` 创建独立的 Git worktree 和 task 分支
7. `Global Leader` 为该 `Task` 启动一个 `Task Orchestrator`
8. `Task Orchestrator` 读取 `PROJECT-DEVELOPER-GUIDE.md`、`Sprint` 上下文和 task，并开始该 task 的局部执行循环
9. `Task Orchestrator` 启动 `Developer`
10. `Developer` 开发任务并完成自测，然后将结构化结果返回给 `Task Orchestrator`
11. `Task Orchestrator` 根据 `Developer` 的结果决定是继续修复、进入 `QA`，还是直接标记为 `blocked`
12. `Task Orchestrator` 启动 `QA`
13. `QA` 执行静态分析和测试，并将结构化结果返回给 `Task Orchestrator`
14. `Task Orchestrator` 根据 `QA` 结果决定是返回 `Developer` 修复，还是进入 `Reviewer`
15. `Task Orchestrator` 按审查策略启动一个或多个并发 `Reviewer` session
16. 每个 `Reviewer` 从各自审查视角评估代码质量和测试覆盖情况，并返回结构化 findings
17. `Task Orchestrator` 对多个 `Reviewer` 的 findings 执行去重、聚合和分级，然后决定是返回 `Developer` 修复，还是创建 `Task PR` 并推送代码到对应的 `Sprint Branch`
18. `Task PR` 触发 CI
19. 如果 CI 失败，则创建 Issue，由 `Task Orchestrator` 结合失败原因、重试次数和失败指纹决定是否返回 `Developer`
20. 如果达到循环上限、重复命中同类失败或长时间无进展，则由 `Task Orchestrator` 将 task 标记为 `escalated` 或 `blocked`
21. 如果 `Task PR` 的 CI 通过，则该 PR 必须自动合并到对应的 `Sprint Branch`
22. `Task PR` 自动合并完成后，由 `Task Orchestrator` 将结果回报给 `Global Leader`
23. `Global Leader` 更新该 `Task` 的状态，并检查所属 `Sprint` 是否已全部完成
24. 如果该 `Sprint` 下仍有未完成的 `Task`，则下一个 `Task` 从最新的 `Sprint Branch` 继续开发
25. 如果该 `Sprint` 下所有 `Task` 均已完成，则由 `Global Leader` 先在该 `Sprint Branch` 上启动并发 `Reviewer` 审查并聚合结果，再创建 `Sprint PR`
26. `Sprint PR` 面向主干分支，禁止自动合并，必须经过人工审查和人工合并
27. `Sprint PR` 合并完成后，`Global Leader` 将该 `Sprint` 标记为 `done`
28. `Global Leader` 清理资源并继续处理下一个 `Sprint` 或 `Task`
