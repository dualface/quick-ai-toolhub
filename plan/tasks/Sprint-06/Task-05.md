# [Sprint-06][Task-05] 实现人工接管收口

## Goal

统一 `needs-human`、`awaiting_human`、`blocked`、`escalated` 等场景下的交接材料生成和恢复入口。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `SPEC-V1.md`
- `TECH-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-05/Task-04`
- `Sprint-06/Task-03`
- `Sprint-06/Task-04`

## In Scope

- 生成 handoff 摘要
- 统一 `needs-human` 触发流程
- 统一 `needs-human` 解除和恢复执行流程
- 保存建议动作、原因和关键引用

## Out of Scope

- 人工审批界面
- Issue 列表管理
- 新的状态机设计

## Deliverables

- 人工接管收口实现
- handoff 数据结构
- 触发和恢复流程测试

## Acceptance Criteria

- 进入人工处理时能产出清晰交接材料
- 恢复执行时有统一入口，而不是分散特殊逻辑
- 本地状态、时间线日志和 GitHub 提示可以保持一致

## Notes

- 该任务负责收口人工流程，不负责决定什么时候必须人工介入
