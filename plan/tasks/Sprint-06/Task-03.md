# [Sprint-06][Task-03] 实现 timeline-log-tool

## Goal

为每个 `Sprint` 维护一份人类可读的时间线日志，记录关键动作、状态变化和人工接管信息。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `TECH-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-01/Task-05`

## In Scope

- 创建 `logs/<sprint>.log`
- 实现普通日志追加
- 实现状态变化、错误、人工交接的专用追加接口
- 统一日志写入格式

## Out of Scope

- 作为状态机真源
- 复杂日志检索系统
- 结构化事件存储

## Deliverables

- `timeline-log-tool` 实现
- 基础日志追加测试

## Acceptance Criteria

- 输出结构符合 `TECH-V1.md`
- 每个 `Sprint` 有独立日志文件
- 状态变化、错误、人工交接都有明确记录格式

## Notes

- 时间线日志只做人类审计入口，不参与调度决策
