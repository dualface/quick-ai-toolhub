# [Sprint-04][Task-02] 实现 Task Orchestrator 阶段机

## Goal

实现单个 task 内部的阶段推进和回退逻辑，打通 `developer -> qa -> review` 闭环。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `SPEC-V1.md`
- `TECH-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-04/Task-01`
- `Sprint-03/Task-03`

## In Scope

- 实现 `developer`、`qa`、`review` 阶段推进
- 根据 Agent 结果决定回退或进入下一阶段
- 将关键阶段结果写事件并更新本地状态
- 保持单 task 内的流程控制权只在 `Task Orchestrator`

## Out of Scope

- `Task PR` 创建
- `GitHub Actions` 读取
- `Sprint PR` 流程

## Deliverables

- `Task Orchestrator` 阶段机实现
- 阶段推进和回退测试

## Acceptance Criteria

- 能用 mock Agent 跑通单 task 的基础阶段闭环
- 阶段转移符合 `README.md` 状态机约束
- 每个阶段都产出统一结构化结果

## Notes

- PR 和 CI 在后续 Sprint 再接入，本任务先把本地闭环跑通
