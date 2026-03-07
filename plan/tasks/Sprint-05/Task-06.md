# [Sprint-05][Task-06] 实现 issue 评论与标签维护

## Goal

统一处理 issue 评论追加和 `needs-human` 标签维护，为 task 生命周期中的人工提示动作提供一致入口。

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
- 输出符合契约的结果摘要
- 评论和标签维护测试或 fixture 测试

## Out of Scope

- 关闭或重开 issue
- issue 读取同步
- 人工介入判定策略

## Deliverables

- issue comment / label maintenance 实现
- 评论与标签测试

## Acceptance Criteria

- 输出结构符合 `TECH-V1.md`
- 评论和标签维护统一通过工具入口触发
- `needs-human` 仅作为提示，不替代本地状态机

## Notes

- 工具层只做维护动作，不做是否需要人工介入的判定
