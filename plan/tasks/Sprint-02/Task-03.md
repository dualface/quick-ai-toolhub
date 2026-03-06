# [Sprint-02][Task-03] 实现 github-sync-tool 增量同步

## Goal

支持通过 webhook 和定向对账对单个 issue / PR / CI 变更做增量同步，减少对全量刷新依赖。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `SPEC-V1.md`
- `TECH-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-02/Task-02`

## In Scope

- 接收 GitHub webhook 事件
- 按 issue、PR、CI run 做定向对账
- 处理重复 webhook 的幂等
- 将增量变化投影到本地并记录结构化事件

## Out of Scope

- `Global Leader` 调度
- outbox 写 GitHub
- 人工处理流程

## Deliverables

- webhook 接收与解析代码
- `ingest_webhook` 和定向 `reconcile_*` 实现
- 重复事件和定向同步测试

## Acceptance Criteria

- issue / PR / CI 变更可触发定向同步
- 重复 webhook 不会导致重复副作用
- 增量同步后的本地投影与全量对账结果一致

## Notes

- webhook 事件先用于更新本地投影，不直接驱动复杂业务动作
