# [Sprint-05][Task-04] 实现 issue-maintenance-tool

## Goal

统一处理 issue 评论、`needs-human` 标签维护和 task issue 关闭等维护动作。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `SPEC-V1.md`
- `TECH-V1.md`
- `TOOLS-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-05/Task-01`

## In Scope

- 追加 issue 评论
- 设置或移除 `needs-human`
- 在 task 完成时关闭 task issue
- 输出符合契约的结果摘要

## Out of Scope

- issue 读取同步
- `Sprint PR` 创建
- 人工交接内容生成

## Deliverables

- `issue-maintenance-tool` 实现
- 评论、标签、关闭 issue 的测试或 fixture 测试

## Acceptance Criteria

- 输出结构符合 `TECH-V1.md`
- issue 维护动作统一通过工具入口触发
- `needs-human` 仅作为提示，不替代本地状态机

## Notes

- 工具层只做维护动作，不做是否需要人工介入的判定
