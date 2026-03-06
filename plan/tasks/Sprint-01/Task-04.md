# [Sprint-01][Task-04] 实现 task-state-store-tool

## Goal

实现 `task-state-store-tool` 的最小闭环，统一读写事件、任务投影、Sprint 投影和 outbox。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `TECH-V1.md`
- `TOOLS-V1.md`
- `sql/schema.sql`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-01/Task-03`

## In Scope

- 实现 `append_event`
- 实现 `load_task_projection`
- 实现 `load_sprint_projection`
- 实现 `update_task_state`
- 实现 `update_sprint_state`
- 实现 `enqueue_outbox_action`
- 实现 `load_pending_outbox_actions`

## Out of Scope

- GitHub 同步逻辑
- 状态机合法性判断
- PR、CI、review findings 的通用写入接口

## Deliverables

- `task-state-store-tool` 接口
- 基于 `Bun` 的 `SQLite` 实现
- 幂等、partial update、pending outbox 的单元测试

## Acceptance Criteria

- 输入输出契约符合 `TECH-V1.md`
- `append_event` 和 `enqueue_outbox_action` 按 `idempotency_key` 去重
- `update_task_state` 和 `update_sprint_state` 仅更新传入字段
- `load_pending_outbox_actions` 只返回当前可执行动作

## Notes

- `events` 是 append-only，禁止通过更新或删除绕过约束
