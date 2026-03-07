# [Sprint-04][Task-03] 实现 review-aggregation-tool

## Goal

实现纯聚合的 `review-aggregation-tool`，对多个 `Reviewer` 的 findings 完成归一化、去重、置信度提升、冲突识别和统一结论计算。

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

- finding 归一化与输入校验
- 按 `finding_fingerprint` 去重
- 处理多 reviewer 同问题命中时的置信度提升
- 识别冲突 finding 与补充审查信号
- 输出结构化聚合结果和摘要

## Out of Scope

- `Task Orchestrator` 的状态推进和回退
- `awaiting_human` / `review_failed` / `pr_open` 状态写入
- `Sprint` 级 reviewer 审查流
- findings 的 GitHub 评论落地
- 人工裁决流程

## Deliverables

- `review-aggregation-tool` 纯聚合实现
- findings 聚合与冲突识别测试

## Acceptance Criteria

- 聚合行为符合 `README.md` 的 reviewer 并发与聚合规则
- `critical` finding 会明确阻塞
- 冲突结论和补充审查信号会被识别而不是静默吞掉
- 调用方无需解析 `summary` 文本，即可通过结构化字段识别 `conflict`、`reviewer_escalation`、`blocking`、`supplemental_review`
- `decision` 优先级与 `TECH-V1.md` 一致：`conflict/reviewer_escalation` 高于 `blocking`，`blocking` 高于纯 `supplemental_review`

## Notes

- 工具只负责聚合和结论，不直接做流程调度
- 该任务优先沉淀纯函数或无副作用接口，便于后续 orchestrator 复用
- 本任务内需要把公共 tool contract 一并定稿，避免调用方通过解析 `summary` 字符串推断冲突或补充审查信号
