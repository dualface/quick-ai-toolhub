# [Sprint-02][Task-02] 实现 github-sync-tool 全量对账

## Goal

实现 `github-sync-tool` 的全量对账路径，将 GitHub 上的任务定义和执行投影完整同步到本地 `SQLite`。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `SPEC-V1.md`
- `TECH-V1.md`
- `sql/schema.sql`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-01/Task-04`
- `Sprint-02/Task-01`

## In Scope

- 读取所有 `open` 的 `kind/sprint` issues 及其 `sub-issues`
- 读取依赖关系、PR 投影和 CI 投影
- 解析 `Sprint-XX`、`Task-XX` 编号和关键正文字段
- 将结果写入本地投影表

## Out of Scope

- webhook 增量更新
- `Global Leader` 调度选择
- GitHub 写操作

## Deliverables

- `full_reconcile` 实现
- 投影写入逻辑
- 对账测试数据和测试

## Acceptance Criteria

- 本地 `sprints`、`tasks`、`task_dependencies`、`pull_requests`、`ci_runs` 可被全量刷新
- 无效编号、孤立 task、跨 Sprint 依赖会被识别
- 对账结果可供 `get-task-list-tool` 直接消费

## Notes

- 行为以 `SPEC-V1.md` 的 GitHub 同步与投影规则为准
