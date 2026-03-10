# [Sprint-06][Task-01] 实现 Sprint reviewer 调度

## Goal

在创建 `Sprint PR` 前，对 `Sprint Branch` 启动单个 reviewer 审查，为后续 `Sprint PR` 前置判定提供统一输入。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `TECH-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-04/Task-01`
- `Sprint-04/Task-03`
- `Sprint-05/Task-03`

## In Scope

- 在 `Sprint Branch` 上启动单个 reviewer
- 按 `README.md` / `TECH-V1.md` 的单 reviewer 规则选择合适 lens
- 收集 reviewer 结果和 artifact refs
- 为后续收口阶段输出统一输入

## Out of Scope

- reviewer 结果 contract 收口
- 形成最终 `Sprint PR` 前置审查结论
- 创建 `Sprint PR`
- 人工审查和人工合并
- task 级 reviewer 流程

## Deliverables

- `Sprint` 级 reviewer 调度实现
- reviewer lens 选择规则实现
- reviewer 运行结果
- 单 reviewer 调度测试

## Acceptance Criteria

- 审查流满足 `README.md` 的 `Sprint PR` 前置规则
- 单个 reviewer 可以稳定运行并产出结构化结果
- 默认以 `architecture` 作为 `Sprint` 级 reviewer lens，除非文档另有明确覆盖规则
- reviewer 结果可被后续收口任务直接消费

## Notes

- 该任务补的是 `Sprint PR` 前的单 reviewer 审查缺口，不要隐式塞进 `sprint-pr-tool`
