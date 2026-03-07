# [Sprint-06][Task-02] 实现 Sprint 级 review 聚合

## Goal

聚合 `Sprint` 级 reviewer findings，形成 `Sprint PR` 创建前的统一审查结论和摘要。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `TECH-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-06/Task-01`
- `Sprint-04/Task-03`

## In Scope

- 聚合 `Sprint` reviewer findings
- 输出 `Sprint PR` 前置审查结论
- 生成 `Sprint` 级结构化摘要
- 补充 Sprint review 聚合测试

## Out of Scope

- 创建 `Sprint PR`
- 人工 review 行为本身
- 自动合并

## Deliverables

- Sprint review 聚合实现
- 聚合结果摘要与测试

## Acceptance Criteria

- 输出结构符合 `TECH-V1.md`
- 审查聚合结果可被 `sprint-pr-tool` 直接消费
- 冲突和阻塞 finding 不会被静默吞掉

## Notes

- 本任务只做审查聚合，不隐式承担 `Sprint PR` 创建职责
