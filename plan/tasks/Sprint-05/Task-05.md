# [Sprint-05][Task-05] 实现 CI 决策映射

## Goal

将归一化后的 CI 视图映射为 `Task Orchestrator` 可直接消费的 `ci_passed`、`ci_failed`、`in_progress` 等决策输入。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `TECH-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-05/Task-04`
- `Sprint-04/Task-02`

## In Scope

- 将 CI 读取结果映射为稳定的决策输入
- 区分成功、失败、仍在运行和无结果
- 输出 `Task Orchestrator` 可直接消费的结构化结果
- 补充 CI 决策映射测试

## Out of Scope

- 直接读取 GitHub Actions
- 触发 CI
- `Sprint PR` 级 CI 流程

## Deliverables

- CI 决策映射实现
- 状态映射测试

## Acceptance Criteria

- `Task Orchestrator` 可以清晰区分 `ci_passed`、`ci_failed`、`in_progress`
- 同一 PR 下多个 run 不会导致决策抖动
- 决策层不泄露过多 GitHub 原始细节

## Notes

- 本任务消费 `Task-04` 的归一化输出，不重新实现 GitHub Actions 读取
