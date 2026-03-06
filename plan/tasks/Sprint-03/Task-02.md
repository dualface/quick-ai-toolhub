# [Sprint-03][Task-02] 实现 git adapter

## Goal

封装本地 `git` 读写操作，为分支管理和 worktree 管理提供统一接口。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `TECH-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-01/Task-05`

## In Scope

- 封装 `fetch`
- 封装分支存在性检查和创建
- 封装 `worktree add/list/remove`
- 封装 `rev-parse`、`checkout`、`push`

## Out of Scope

- `prepare-worktree-tool` 的业务决策
- GitHub PR 操作
- 调度状态写库

## Deliverables

- `internal/git` adapter
- 基于临时仓库的 git adapter 测试

## Acceptance Criteria

- 常见 git 操作有统一接口
- 调用方无需直接拼接命令字符串
- worktree 相关行为可在测试中稳定验证

## Notes

- 本任务只做本地 `git` 能力，不做 GitHub 领域封装
