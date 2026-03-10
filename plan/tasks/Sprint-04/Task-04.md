# [Sprint-04][Task-04] 接入 reviewer 结果收口与人工接管流

## Goal

将 `review-result-tool` 接入 `Task Orchestrator` 的 review 阶段，消费结构化 `reviewer_escalation` / `blocking` signals，落实退回开发、人工接管和统一流程决策。

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

- 在 review 阶段调用 `review-result-tool`
- 基于结构化 reviewer 结果决定 `return_to_developer` / `awaiting_human` / `open_task_pr`
- 将 `review-result-tool` 的 `decision` 明确映射到 `review_passed` / `review_changes_requested` / `review_awaits_human`
- 接入 reviewer 明确升级人工时的 handoff 分支
- 选择最合适的 `artifact_refs` 和结构化摘要
- 补齐 orchestrator 侧 review 决策回归测试

## Out of Scope

- failure budget 和无进展检测
- `Sprint` 级 reviewer 审查流
- findings 的 GitHub 评论落地
- 人工裁决执行流程

## Deliverables

- review 结果接线实现
- review 阶段人工接管与证据选择测试

## Acceptance Criteria

- `Task Orchestrator` 的流程控制权仍只在 orchestrator
- reviewer 要求修复时会稳定回到 `Developer`
- reviewer 明确升级人工时会稳定进入 `awaiting_human`
- orchestrator 只消费 `review-result-tool` 的结构化 `decision`，不再直接解析 reviewer 原始 `status`
- reviewer 结果不会直接绕过 contract 收口逻辑决定流程走向

## Notes

- 本任务关注“如何消费单 reviewer 的结构化 contract”
