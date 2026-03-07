# [Sprint-06][Task-01] 实现 Sprint reviewer 并发调度

## Goal

在创建 `Sprint PR` 前，对 `Sprint Branch` 启动异质化并发 reviewer 审查，为后续聚合结论提供统一输入。

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
- 收集 reviewer 结果和 artifact refs
- 为后续聚合阶段输出统一输入

## Out of Scope

- 聚合 `Sprint` 级 findings
- 形成最终 `Sprint PR` 前置审查结论
- 创建 `Sprint PR`
- 人工审查和人工合并
- task 级 reviewer 流程

## Deliverables

- `Sprint` 级 reviewer 调度实现
- reviewer 运行结果集合
- 并发 reviewer 测试

## Acceptance Criteria

- 审查流满足 `README.md` 的 `Sprint PR` 前置规则
- 至少两个 reviewer 并发运行且视角不重复
- reviewer 结果集合可被后续聚合任务直接消费

## Notes

- 该任务补的是 `Sprint PR` 前的审查缺口，不要隐式塞进 `sprint-pr-tool`
