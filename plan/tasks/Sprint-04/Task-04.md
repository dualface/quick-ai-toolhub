# [Sprint-04][Task-04] 接入 review 聚合与补充审查流

## Goal

将 `review-aggregation-tool` 接入 `Task Orchestrator` 的 review 阶段，消费结构化 `conflict` / `supplemental_review` / `blocking` signals，落实补充审查、证据选择和统一流程决策。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `SPEC-V1.md`
- `TECH-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-04/Task-02`
- `Sprint-04/Task-03`

## In Scope

- 在 review 阶段调用 `review-aggregation-tool`
- 基于结构化聚合结果决定 `return_to_developer` / `await_human` / `open_task_pr`
- 接入补充审查或二次验证分支
- 选择聚合后最合适的 `artifact_refs` 和结构化摘要
- 补齐 orchestrator 侧 review 聚合回归测试

## Out of Scope

- failure budget 和无进展检测
- `Sprint` 级 reviewer 审查流
- findings 的 GitHub 评论落地
- 人工裁决执行流程

## Deliverables

- review 聚合接线实现
- review 阶段补充审查与证据选择测试

## Acceptance Criteria

- `Task Orchestrator` 的流程控制权仍只在 orchestrator
- 单 reviewer 的中低置信度 finding 会先触发补充审查或二次验证
- reviewer 结果不会直接绕过聚合逻辑决定流程走向

## Notes

- 本任务关注“如何消费结构化聚合 contract”，不再重新实现一套 findings 聚合语义
