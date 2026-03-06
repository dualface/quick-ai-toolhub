# [Sprint-06][Task-01] 实现 Sprint 级 reviewer 审查流

## Goal

在创建 `Sprint PR` 前，对 `Sprint Branch` 启动并发 reviewer 审查并聚合结论，满足架构约束中的前置条件。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `TECH-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-04/Task-01`
- `Sprint-04/Task-03`
- `Sprint-05/Task-03`

## In Scope

- 在 `Sprint Branch` 上启动至少 2 个 reviewer
- 保证 reviewer 视角异质化
- 聚合 `Sprint` 级 findings
- 形成 `Sprint PR` 前置审查结论

## Out of Scope

- 创建 `Sprint PR`
- 人工审查和人工合并
- task 级 reviewer 流程

## Deliverables

- `Sprint` 级 reviewer 调度实现
- 聚合结果摘要
- 并发 reviewer 测试

## Acceptance Criteria

- 审查流满足 `README.md` 的 `Sprint PR` 前置规则
- 至少两个 reviewer 并发运行且视角不重复
- 聚合结果可被 `sprint-pr-tool` 直接消费

## Notes

- 该任务补的是 `Sprint PR` 前的审查缺口，不要隐式塞进 `sprint-pr-tool`
