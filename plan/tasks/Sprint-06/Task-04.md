# [Sprint-06][Task-04] 实现 Leader 恢复与定时对账

## Goal

在进程重启或外部状态漂移后，恢复 `Global Leader` 的本地控制面，并通过定时对账修复投影偏差。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `SPEC-V1.md`
- `TECH-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-02/Task-03`
- `Sprint-03/Task-04`
- `Sprint-06/Task-03`

## In Scope

- 启动时恢复当前 `Sprint` / `Task` 投影
- 恢复正在进行中的工作状态
- 周期性触发轻量全量对账
- 发现漂移时补写事件并修正状态

## Out of Scope

- 多实例 leader 选举
- 分布式锁
- 跨仓库恢复

## Deliverables

- Leader 恢复逻辑
- 定时对账 worker
- 恢复与漂移修正测试

## Acceptance Criteria

- 重启后系统能重新知道当前进行到哪个 `Sprint` / `Task`
- 定时对账能补齐缺失投影或事件
- 发现冲突时先记录对账事件，而不是静默覆盖

## Notes

- 行为以事件驱动恢复规则和 GitHub 同步规则为准
