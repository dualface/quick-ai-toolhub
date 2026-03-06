# [Sprint-03][Task-01] 实现 Leader 调度选择逻辑

## Goal

实现 `Global Leader` 的 `select-next-sprint` 和 `select-next-task` 逻辑，稳定选出下一个可执行任务。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `SPEC-V1.md`
- `TOOLS-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-02/Task-04`

## In Scope

- 选择下一个 `Sprint`
- 选择下一个 `Task`
- 按状态、顺序和显式依赖过滤
- 对无可执行任务的情况返回明确结果

## Out of Scope

- 创建 worktree
- 启动 Agent
- 写 GitHub

## Deliverables

- Leader 选择逻辑实现
- 选择逻辑测试

## Acceptance Criteria

- 在相同输入下选择结果稳定且可预测
- 只会选择满足约束的 `Sprint` / `Task`
- 无可执行任务时不会返回模糊结果

## Notes

- 该逻辑保留在进程内，不单独拆工具
