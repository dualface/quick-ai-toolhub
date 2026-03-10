# TOOLS-V1

本文件只保留 `v1` 的必要工具清单。

## 已保留的显式工具名

- `get-task-list-tool`
- `prepare-worktree-tool`

## P0 必需工具

这些工具构成最小可运行闭环，必须优先实现。

| tool_id                   | owner                                | 作用                                                |
| ------------------------- | ------------------------------------ | --------------------------------------------------- |
| `task-state-store-tool`   | `Global Leader`, `Task Orchestrator` | 统一读写 `SQLite` 中的事件、状态投影和 outbox       |
| `github-sync-tool`        | `Global Leader`                      | 同步 GitHub issue / PR / CI 到本地投影              |
| `get-task-list-tool`      | `Global Leader`                      | 返回可调度的 `Sprint` / `Task` 列表                 |
| `prepare-worktree-tool`   | `Global Leader`                      | 准备 `Sprint Branch`、task branch 和 worktree       |
| `run-agent-tool`          | `Task Orchestrator`                  | 启动 `Developer`、`QA`、`Reviewer` 并收集结构化结果 |
| `review-result-tool`      | `Task Orchestrator`                  | 校验单个 reviewer 结果并给出稳定结论                 |
| `task-pr-tool`            | `Task Orchestrator`                  | 创建 / 更新 `Task PR` 并处理自动合并                |
| `ci-status-tool`          | `Task Orchestrator`                  | 读取 `GitHub Actions` 状态并映射为 task 决策输入    |

## P1 收尾工具

这些工具不影响最小闭环启动，但属于 `v1` 应实现范围。

| tool_id                  | owner                                | 作用                                          |
| ------------------------ | ------------------------------------ | --------------------------------------------- |
| `issue-maintenance-tool` | `Global Leader`, `Task Orchestrator` | 写 issue 评论、加减 `needs-human`、关闭 issue |
| `sprint-pr-tool`         | `Global Leader`                      | 创建并跟踪 `Sprint PR`                        |
| `timeline-log-tool`      | `Global Leader`, `Task Orchestrator` | 追加写 `logs/<sprint>.log`                    |

## 暂不拆工具的逻辑

以下逻辑先保留在进程内：

- `select-next-sprint`
- `select-next-task`
- 循环上限判断
- 无进展检测
- 失败指纹生成
- 状态机合法转移校验

## 一句话顺序

实现顺序建议为：

`task-state-store-tool -> github-sync-tool -> get-task-list-tool -> prepare-worktree-tool -> run-agent-tool -> review-result-tool -> task-pr-tool -> ci-status-tool -> issue-maintenance-tool -> sprint-pr-tool -> timeline-log-tool`
