# [Sprint-05][Task-01] 实现 GitHub outbox worker

## Goal

实现 outbox worker，按 `outbox_actions` 队列异步执行写 GitHub 的动作，并负责幂等、状态更新和重试。

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

- 轮询待执行的 `outbox_actions`
- 调用 `gh` 执行 GitHub 写操作
- 更新 `pending`、`running`、`succeeded`、`failed` 等状态
- 处理重试次数和下次执行时间

## Out of Scope

- 何时创建 outbox action 的业务决策
- GitHub 读取同步
- 调度逻辑

## Deliverables

- outbox worker 实现
- 动作执行器和重试测试

## Acceptance Criteria

- worker 只消费当前可执行动作
- 执行结果会稳定回写 `outbox_actions`
- 重复执行不会造成重复副作用

## Notes

- 所有 GitHub 写操作必须通过该 worker，而不是绕开 outbox 直接写
