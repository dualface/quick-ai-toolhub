# [Sprint-05][Task-04] 实现 ci-status-tool 读取归一化

## Goal

读取 `GitHub Actions` 状态，归一化 run / checks 结果，并输出稳定的 CI 视图供后续决策使用。

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
- 输出稳定的 `ci_run_ref`
- 补充 CI 读取归一化测试

## Out of Scope

- CI 决策映射
- 触发 CI
- CI 日志深度解析
- `Sprint PR` 的人工审查状态

## Deliverables

- `ci-status-tool` 读取归一化实现
- CI 读取与归一化测试

## Acceptance Criteria

- 输出结构符合 `TECH-V1.md`
- 同一 PR 的多个 run 能被稳定归并到统一视图
- CI 未结束和已失败可以被清晰区分

## Notes

- 本任务只沉淀 CI 读取视图，不直接给出阶段机决策
