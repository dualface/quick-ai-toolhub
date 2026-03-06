# GITHUB-CLI-V1

`v1` 的 GitHub 出站操作统一使用 `gh`。

## 结论

- 常规操作使用 `gh issue`、`gh pr`、`gh run`
- 缺少一等命令的能力使用 `gh api`
- 本地分支、提交、rebase、push 仍使用 `git`
- 所有 `gh` 命令必须在目标仓库的 worktree 中执行
- 默认不要求执行 `gh repo set-default`

## 补充约束

- 如果命令运行目录不在目标仓库 worktree 中，必须显式传 `-R <owner>/<repo>`，或先配置 `gh repo set-default`
- 如果本地存在多个 GitHub remote，调用方必须显式指定目标仓库，避免歧义
