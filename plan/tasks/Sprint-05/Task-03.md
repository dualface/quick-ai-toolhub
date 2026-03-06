# [Sprint-05][Task-03] 实现 ci-status-tool

## Goal

读取 `GitHub Actions` 状态，并将其映射成 task 决策可直接消费的 CI 信号。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `TECH-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-02/Task-01`
- `Sprint-05/Task-02`

## In Scope

- 读取 `Task PR` 关联的 CI run 或 checks
- 归一化 `in_progress`、`passed`、`failed` 等状态
- 输出 `ci_run_ref`
- 为阶段机提供 `ci_passed` / `ci_failed` 判断输入

## Out of Scope

- 触发 CI
- CI 日志深度解析
- `Sprint PR` 的人工审查状态

## Deliverables

- `ci-status-tool` 实现
- CI 状态映射测试

## Acceptance Criteria

- 输出结构符合 `TECH-V1.md`
- 同一 PR 的多个 run 能被稳定归并到决策输入
- CI 未结束和已失败可以被清晰区分

## Notes

- 决策输入应尽量稳定，不向上层泄露过多 GitHub 细节
