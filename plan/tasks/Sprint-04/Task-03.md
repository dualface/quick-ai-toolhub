# [Sprint-04][Task-03] 实现 review-result-tool

## Goal

实现 `review-result-tool`，对单个 `Reviewer` 的结构化结果做校验、归一和稳定结论计算。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `TECH-V1.md`
- `TOOLS-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-04/Task-01`
- `Sprint-04/Task-02`

## In Scope

- reviewer result 归一化与输入校验
- finding 字段校验与结构化收口
- 识别 `critical` / `blocking` / `reviewer_escalation`
- 输出结构化 review 结果和摘要

## Out of Scope

- `Task Orchestrator` 的状态推进和回退
- `awaiting_human` / `review_failed` / `pr_open` 状态写入
- `Sprint` 级 reviewer 审查流
- findings 的 GitHub 评论落地
- 人工裁决流程

## Deliverables

- `review-result-tool` 实现
- reviewer 结果校验与决策测试

## Acceptance Criteria

- review 结果收口行为符合 `README.md` 的单 reviewer 审查规则
- `critical` finding 会明确阻塞
- 调用方无需解析 `summary` 文本，即可通过结构化字段识别 `reviewer_escalation`、`blocking`
- `decision` 优先级与 `TECH-V1.md` 一致：`reviewer_escalation` 高于 `blocking`，普通 findings 高于纯 `pass`

## Notes

- 工具只负责单 reviewer 结果的 contract 收口和结论，不直接做流程调度
- 该任务优先沉淀纯函数或无副作用接口，便于后续 orchestrator 复用
- 本任务内需要把公共 tool contract 一并定稿，避免调用方通过解析 `summary` 字符串推断 reviewer 结论
