# [Sprint-06][Task-08] 实现人工接管状态流与恢复入口

## Goal

统一 `needs-human` 的触发、解除和恢复执行入口，让人工接管后的继续执行不再依赖分散的特殊逻辑。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `SPEC-V1.md`
- `TECH-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-05/Task-07`
- `Sprint-06/Task-04`
- `Sprint-06/Task-06`
- `Sprint-06/Task-07`

## In Scope

- 统一 `needs-human` 触发流程
- 统一 `needs-human` 解除和恢复执行入口
- 保持本地状态、时间线日志和 GitHub 提示一致
- 触发和恢复流程测试

## Out of Scope

- 人工审批界面
- issue 列表管理
- 新的状态机设计

## Deliverables

- 人工接管状态流实现
- 恢复执行入口
- 触发和恢复流程测试

## Acceptance Criteria

- 恢复执行时有统一入口，而不是分散特殊逻辑
- 本地状态、时间线日志和 GitHub 提示可以保持一致
- 人工接管流程能与现有 orchestrator / leader 状态机对接

## Notes

- 本任务消费 `Task-07` 产出的交接材料，不重新定义 handoff 数据结构
