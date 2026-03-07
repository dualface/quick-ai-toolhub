# [Sprint-05][Task-02] 实现 task-pr-tool 创建更新能力

## Goal

实现 `Task PR` 的创建、更新和本地 PR 投影回写，打通单个 task 的 GitHub PR 基础交付面。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `TECH-V1.md`
- `TOOLS-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-05/Task-01`
- `Sprint-04/Task-02`

## In Scope

- 生成 `Task PR` 标题和正文
- 创建或更新 `Task PR`
- 回写或同步本地 PR 投影

## Out of Scope

- 自动合并启用和合并状态接线
- `Sprint PR`
- `GitHub Actions` 状态判断
- `Sprint` 级 reviewer 审查

## Deliverables

- `task-pr-tool` 创建更新实现
- PR 创建、更新和投影回写测试或 fixture 测试

## Acceptance Criteria

- 输出结构符合 `TECH-V1.md`
- `Task PR` 流向符合 `task/<sprint>/<task> -> sprint/<sprint>`
- PR 创建与更新结果能稳定映射到本地 PR 投影

## Notes

- 写 GitHub 的动作应通过 outbox 路径执行
- 自动合并能力拆到后续任务实现，避免与 PR 创建更新耦合
