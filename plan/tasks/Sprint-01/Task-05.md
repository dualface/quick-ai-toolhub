# [Sprint-01][Task-05] 建立统一日志与命令执行器

## Goal

建立统一的 `slog JSON` 日志和外部命令执行器，为后续 `gh` / `git` adapter 提供公共基础。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `TECH-V1.md`
- `GITHUB-CLI-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-01/Task-01`
- `Sprint-01/Task-02`

## In Scope

- 初始化统一的 `slog` logger
- 封装外部命令执行器
- 支持 `workdir`、环境变量、超时和结构化结果
- 统一处理 `stdout`、`stderr`、退出码和执行错误

## Out of Scope

- 任何 GitHub 领域逻辑
- 任何 Git 领域逻辑
- 时间线日志文件追加逻辑

## Deliverables

- 日志初始化代码
- 外部命令执行器
- 针对命令成功、失败、超时的测试

## Acceptance Criteria

- 进程日志默认输出 `JSON`
- 命令执行器可以在指定 worktree 中执行外部命令
- 调用方可以稳定拿到 `stdout`、`stderr`、退出码和错误

## Notes

- 后续所有 `gh` / `git` 调用应复用该执行器
