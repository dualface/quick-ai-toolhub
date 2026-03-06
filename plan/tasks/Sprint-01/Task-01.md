# [Sprint-01][Task-01] 初始化 Go 工程骨架

## Goal

建立 `v1` 的最小 Go 工程骨架和启动入口，为后续配置、store、Leader、Orchestrator 实现提供稳定基座。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `TECH-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- None

## In Scope

- 初始化 `Go` module 和基础依赖管理
- 建立 `cmd/toolhub/` 入口
- 建立 `internal/leader`、`internal/orchestrator`、`internal/store`、`internal/github`、`internal/git`、`internal/timeline` 等基础目录
- 搭建最小启动流程和应用装配骨架

## Out of Scope

- `YAML` 配置加载
- 数据库初始化和 store 逻辑
- 任何具体工具实现

## Deliverables

- `go.mod`
- `cmd/toolhub/` 最小可编译入口
- 关键 `internal/` 包骨架

## Acceptance Criteria

- `go build ./...` 可以通过
- 存在清晰的应用入口和初始化流程
- 后续任务可以在现有目录骨架上继续实现，无需重整结构

## Notes

- 目录结构应与 `TECH-V1.md` 中的建议目录保持一致
