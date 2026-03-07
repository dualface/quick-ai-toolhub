# [Sprint-05][Task-07] 实现 task issue 关闭收尾

## Goal

在 task 完成或需要恢复时，统一处理 task issue 的关闭或重开收尾动作，并保持本地维护结果可追踪。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `SPEC-V1.md`
- `TECH-V1.md`
- `TOOLS-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-05/Task-01`
- `Sprint-05/Task-06`

## In Scope

- 在 task 完成时关闭 task issue
- 在必要时支持重开 task issue
- 输出维护结果摘要
- 关闭 / 重开 issue 测试或 fixture 测试

## Out of Scope

- issue 评论追加
- `needs-human` 标签维护
- `Sprint` issue 收尾

## Deliverables

- task issue close / reopen 实现
- 关闭收尾测试

## Acceptance Criteria

- 完成态 task issue 能被稳定关闭
- 恢复执行时可以重新打开对应 task issue
- issue 收尾动作不会绕过统一工具入口

## Notes

- 本任务只做 issue 关闭收尾，不重新承载评论和标签维护逻辑
