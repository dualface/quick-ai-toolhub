# [Sprint-02][Task-01] 实现 GitHub CLI adapter

## Goal

封装基于 `gh` 的 GitHub 读取能力，并将输出归一化为项目内部可用的数据结构。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `GITHUB-CLI-V1.md`
- `TECH-V1.md`
- `SPEC-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-01/Task-05`

## In Scope

- 封装 `gh issue` 读取
- 封装 `gh pr` 读取
- 封装 `gh run` 读取
- 封装 `gh api` 读取 `sub-issues` 和 `issue dependencies`
- 归一化输出为内部结构体

## Out of Scope

- GitHub 写操作
- outbox worker
- 投影写库逻辑

## Deliverables

- `internal/github` 下的 `gh` 读取 adapter
- 关键 GitHub 实体的内部数据结构
- 适配器层测试或基于 fixture 的解析测试

## Acceptance Criteria

- adapter 可以覆盖 `Sprint issue`、`Task issue`、PR、CI run 的读取需要
- 缺少一等命令的能力统一走 `gh api`
- 读取结果对上层工具不暴露原始命令行细节

## Notes

- 所有 `gh` 命令必须支持在指定 worktree 中执行
