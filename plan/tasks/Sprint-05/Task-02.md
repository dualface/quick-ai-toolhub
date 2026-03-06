# [Sprint-05][Task-02] 实现 task-pr-tool

## Goal

实现 `Task PR` 的创建、更新和自动合并接入，打通单个 task 的 GitHub 交付面。

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
- 启用自动合并
- 回写或同步本地 PR 投影

## Out of Scope

- `Sprint PR`
- `GitHub Actions` 状态判断
- `Sprint` 级 reviewer 审查

## Deliverables

- `task-pr-tool` 实现
- PR 创建、更新、自动合并测试或 fixture 测试

## Acceptance Criteria

- 输出结构符合 `TECH-V1.md`
- `Task PR` 流向符合 `task/<sprint>/<task> -> sprint/<sprint>`
- 自动合并只在满足条件时启用

## Notes

- 写 GitHub 的动作应通过 outbox 路径执行
