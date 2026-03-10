# [Sprint-06][Task-02] 实现 Sprint review 结果收口

## Goal

对单个 `Sprint` reviewer 结果做校验、归一和稳定结论收口，形成 `Sprint PR` 创建前的统一审查结论和摘要。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `TECH-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-06/Task-01`
- `Sprint-04/Task-03`

## In Scope

- 校验 `Sprint` reviewer 结果
- 输出 `Sprint PR` 前置审查结论
- 生成 `Sprint` 级结构化摘要
- 补充 Sprint review 收口测试

## Out of Scope

- 创建 `Sprint PR`
- 人工 review 行为本身
- 自动合并

## Deliverables

- Sprint review 结果收口实现
- 审查结果摘要与测试

## Acceptance Criteria

- 输出结构符合 `TECH-V1.md`
- 审查结果可被 `sprint-pr-tool` 直接消费
- reviewer escalation 和阻塞 finding 不会被静默吞掉

## Notes

- 本任务只做单 reviewer 审查结果收口，不隐式承担 `Sprint PR` 创建职责
