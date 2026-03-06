# [Sprint-04][Task-01] Pre-QA Checklist

本清单用于在提交 `QA` 之前做开发侧自检，减少反复发现新的系统性问题。

## 使用方式

- 完成代码改动后，先逐项检查本清单
- 仅当本清单全部通过，才进入 `QA`
- 如果某项因为环境限制无法验证，必须在结果中明确记录

## CLI 输出

- 非 `--stream` 模式输出为人读格式，不直接打印 JSON
- `--stream` 模式仍能输出完整流式内容
- 默认显示低噪声 progress
- `--no-progress` 可以关闭 progress
- `--stream` 启用时会自动禁用 progress

## 结构化结果

- 成功路径返回完整结构化结果
- 失败路径返回完整结构化结果，而不是只返回顶层错误
- `artifact_refs.log`、`artifact_refs.worktree`、`artifact_refs.report` 可被下游直接消费
- agent 自己提供的 `artifact_refs.report` 不会被本地 `result.json` 覆盖

## Schema 与解析

- runner 输出必须满足 `TECH-V1.md` 中 `run-agent-tool` 的 schema
- `failure_fingerprint`、`artifact_refs`、`findings` 缺失时会被判为 `malformed_output`
- nullable 字段可以为 `null`，但不能省略
- findings 子项也按 schema-required 字段校验

## 权限与路径

- `developer`、`qa` 显式使用 `workspace-write`
- `reviewer` 显式使用 `read-only`
- worktree 外产物目录会自动追加 `--add-dir`
- reviewer 运行所需的 runner 输出目录不会落在 worktree 内
- 并发 reviewer 运行不会写到同一个 run 目录

## 运行目录与产物

- 每次运行目录都唯一
- run 目录至少包含 `prompt.md`、`output-schema.json`、`runner.log`、`result.json`
- 最终 `result.json` 与终端摘要保持一致

## Go 运行环境

- `developer`、`qa` 运行会注入 repo-local `TMPDIR`
- `developer`、`qa` 运行会注入 repo-local `GOTMPDIR`
- `developer`、`qa` 运行会注入 repo-local `GOCACHE`
- 不依赖 `/tmp` 才能执行 Go 构建类命令

## 最小验证

- `go test ./...`
- 至少一条 `run-task` 成功路径验证
- 至少一条失败或异常路径验证
- `reviewer` 权限路径验证
- worktree 外 `output-root` 验证

## 备注

- 如果仓库没有定义 lint 工具，不要把“机器上缺少全局 lint 命令”直接当成代码缺陷
- 如果验证失败是纯环境问题，应标记为 `verification_gap` 或 `blocked`，不要伪装成实现缺陷
