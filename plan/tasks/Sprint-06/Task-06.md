# [Sprint-06][Task-06] 实现 Leader 定时对账修复

## Goal

周期性执行轻量全量对账，发现投影漂移后补写事件并修正状态，保持 `Global Leader` 控制面与外部状态一致。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `SPEC-V1.md`
- `TECH-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-02/Task-03`
- `Sprint-03/Task-03`
- `Sprint-06/Task-04`
- `Sprint-06/Task-05`

## In Scope

- 周期性触发轻量对账
- 发现漂移时补写事件并修正状态
- 记录对账修复测试

## Out of Scope

- 启动恢复
- 多实例 leader 选举
- 分布式锁
- 跨仓库恢复

## Deliverables

- 定时对账 worker
- 漂移修正测试

## Acceptance Criteria

- 定时对账能补齐缺失投影或事件
- 发现冲突时先记录对账事件，而不是静默覆盖
- 修复逻辑可在单进程 leader 下稳定运行

## Notes

- 本任务消费启动恢复后的基础状态，不负责重建整个启动恢复流程
