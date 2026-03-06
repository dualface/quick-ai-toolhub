# [Sprint-02][Task-04] 实现 get-task-list-tool

## Goal

基于本地投影输出当前可调度的 `Sprint` / `Task` 列表以及阻塞原因，作为 `Global Leader` 的输入。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `SPEC-V1.md`
- `TECH-V1.md`
- `TOOLS-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-02/Task-02`
- `Sprint-02/Task-03`

## In Scope

- 读取本地 `Sprint` / `Task` 投影
- 按业务编号排序
- 计算显式依赖阻塞
- 输出 `blocking_issues`

## Out of Scope

- 实际选择下一个 task 的策略实现
- worktree 准备
- Agent 启动

## Deliverables

- `get-task-list-tool` 实现
- 相关查询代码
- 顺序、阻塞和异常输入测试

## Acceptance Criteria

- 输出结构符合 `TECH-V1.md`
- `Sprint` 和 `Task` 顺序只由业务编号决定
- 孤立 task、依赖未解除、无效输入会体现在阻塞结果中

## Notes

- 工具负责“列出可调度集合”，不负责“最终选择哪个 task”
