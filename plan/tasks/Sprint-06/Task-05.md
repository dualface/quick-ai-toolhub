# [Sprint-06][Task-05] 实现 Leader 启动恢复

## Goal

在进程重启后恢复 `Global Leader` 的本地控制面，重新知道当前进行到哪个 `Sprint` / `Task` 以及进行中的工作状态。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `SPEC-V1.md`
- `TECH-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-02/Task-03`
- `Sprint-03/Task-04`
- `Sprint-06/Task-04`

## In Scope

- 启动时恢复当前 `Sprint` / `Task` 投影
- 恢复正在进行中的工作状态
- 补充启动恢复测试

## Out of Scope

- 周期性定时对账
- 漂移修正策略
- 多实例 leader 选举
- 分布式锁
- 跨仓库恢复

## Deliverables

- Leader 启动恢复逻辑
- 启动恢复测试

## Acceptance Criteria

- 重启后系统能重新知道当前进行到哪个 `Sprint` / `Task`
- 正在进行中的工作状态可以被重新接管
- 恢复行为与事件驱动状态机保持一致

## Notes

- 本任务只做启动恢复，不承担周期性漂移修复
