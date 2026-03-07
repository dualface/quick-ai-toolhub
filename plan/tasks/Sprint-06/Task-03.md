# [Sprint-06][Task-03] 实现 sprint-pr-tool

## Goal

创建并跟踪 `Sprint PR`，等待人工审查与手动合并，完成 `Sprint` 级交付面接入。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `TECH-V1.md`
- `TOOLS-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-06/Task-02`
- `Sprint-05/Task-01`

## In Scope

- 创建 `Sprint PR`
- 读取或同步 `Sprint PR` 当前状态
- 将状态映射到本地 `Sprint` 投影
- 保持 `Sprint PR` 禁止自动合并

## Out of Scope

- `Sprint PR` 前 reviewer 审查
- 人工 review 行为本身
- 自动合并

## Deliverables

- `sprint-pr-tool` 实现
- PR 创建与状态同步测试

## Acceptance Criteria

- 输出结构符合 `TECH-V1.md`
- `Sprint PR` 面向主干分支且不启用自动合并
- 本地 `Sprint` 状态可反映 `Sprint PR` 的当前状态

## Notes

- 用户负责人工审查和人工合并，该工具只负责创建和跟踪
