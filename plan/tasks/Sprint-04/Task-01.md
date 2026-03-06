# [Sprint-04][Task-01] 实现 run-agent-tool

## Goal

建立统一的 Agent 调用接口和结果收集能力，为 `Developer`、`QA`、`Reviewer` 提供一致的运行入口。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `TECH-V1.md`
- `TOOLS-V1.md`
- `AGENT-CLI-V1.md`
- `plan/SPRINTS-V1.md`
- `plan/tasks/Sprint-04/Task-01-CHECKLIST.md`

## Dependencies

- `Sprint-01/Task-05`
- `Sprint-03/Task-04`

## In Scope

- 定义 Agent 调用接口
- 支持 `codex_exec`
- 支持超时控制和错误归类
- 显式处理 `codex_exec` 的权限策略
- 收集结构化结果和 `artifact_refs`
- 为不同角色传入统一上下文载荷

## Out of Scope

- 具体 `Developer` / `QA` / `Reviewer` 提示词设计
- 阶段机状态推进
- PR 和 CI 逻辑

## Deliverables

- `run-agent-tool` 实现
- Agent 请求和响应结构
- mock Agent 测试

## Acceptance Criteria

- 输出结构符合 `TECH-V1.md`
- 默认 runner 为 `codex_exec`
- 不依赖用户本机默认权限配置
- 调用方可以稳定区分成功、失败、超时
- `artifact_refs` 和结构化摘要可被阶段机直接消费

## Notes

- 该工具应优先为可测试性设计，避免与具体 Agent 提供方强耦合
- `v1` 先只实现 `codex_exec`
- 进入 `QA` 前先完整执行 `Task-01-CHECKLIST.md`
