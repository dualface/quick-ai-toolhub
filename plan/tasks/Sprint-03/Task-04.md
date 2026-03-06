# [Sprint-03][Task-04] 打通 Task 启动状态流

## Goal

在 task 正式启动时写入必要事件、更新投影状态，并持久化 worktree / branch 引用，形成可恢复的起点。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `TECH-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-01/Task-04`
- `Sprint-03/Task-03`

## In Scope

- 写入 task 选择和启动相关事件
- 更新 task 和 Sprint 的当前状态
- 保存 task branch、Sprint branch、worktree 引用
- 保证重复启动时行为可恢复

## Out of Scope

- `Developer` / `QA` / `Reviewer` 执行
- PR 和 CI 逻辑
- 人工接管

## Deliverables

- task 启动状态流实现
- 事件与投影更新测试

## Acceptance Criteria

- task 启动后，数据库中存在完整的恢复所需引用
- 事件和投影更新顺序符合项目约束
- 重复触发启动不会导致脏状态或重复副作用

## Notes

- 写状态前先写事件，遵守事件驱动约束
