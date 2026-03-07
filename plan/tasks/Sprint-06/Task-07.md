# [Sprint-06][Task-07] 实现人工交接材料生成

## Goal

在 `needs-human`、`awaiting_human`、`blocked`、`escalated` 等场景下，生成统一的 handoff 摘要和关键引用，便于人工快速接手。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `SPEC-V1.md`
- `TECH-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-05/Task-06`
- `Sprint-06/Task-04`

## In Scope

- 生成 handoff 摘要
- 保存建议动作、原因和关键引用
- 统一 handoff 数据结构
- handoff 生成测试

## Out of Scope

- `needs-human` 状态切换
- 恢复执行入口
- 人工审批界面

## Deliverables

- handoff artifact 生成实现
- handoff 数据结构与测试

## Acceptance Criteria

- 进入人工处理时能产出清晰交接材料
- 交接材料可被时间线日志和 GitHub 提示复用
- handoff 数据结构保持统一且可扩展

## Notes

- 本任务只做“材料生成”，不负责改变状态机
