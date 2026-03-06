# [Sprint-04][Task-03] 实现 review-aggregation-tool

## Goal

聚合多个 `Reviewer` 的 findings，完成去重、分级、冲突升级和统一结论输出。

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

- 按 `finding_fingerprint` 去重
- 处理多 reviewer 同问题命中时的置信度提升
- 聚合冲突结论并产出升级信号
- 输出结构化审查结论和摘要

## Out of Scope

- `Sprint` 级 reviewer 审查流
- findings 的 GitHub 评论落地
- 人工裁决流程

## Deliverables

- `review-aggregation-tool` 实现
- reviewer findings 聚合测试

## Acceptance Criteria

- 聚合行为符合 `README.md` 的 reviewer 并发与聚合规则
- `critical` finding 会明确阻塞
- 冲突结论会被识别而不是静默吞掉

## Notes

- 工具只负责聚合和结论，不直接做流程调度
