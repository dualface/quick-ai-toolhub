# [Sprint-03][Task-03] 实现 prepare-worktree-tool

## Goal

实现 `prepare-worktree-tool`，为选中的 task 准备 `Sprint Branch`、task branch 和独立 worktree。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `TECH-V1.md`
- `TOOLS-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-03/Task-01`
- `Sprint-03/Task-02`

## In Scope

- 创建或复用 `Sprint Branch`
- 创建或复用 task branch
- 创建或复用 worktree
- 返回 `base_commit_sha` 和是否复用

## Out of Scope

- task 启动状态事件
- Agent 执行循环
- GitHub 同步

## Deliverables

- `prepare-worktree-tool` 实现
- 相关路径、复用和分支场景测试

## Acceptance Criteria

- 输出结构符合 `TECH-V1.md`
- 重复调用时具备稳定的复用行为
- worktree、分支和基线提交信息可被后续任务直接消费

## Notes

- 分支命名和流向需遵守 `README.md` 的分支策略
