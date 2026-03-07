# [Sprint-04][Task-06] 实现无进展检测与升级建议

## Goal

基于 failure fingerprint、失败计数和阶段历史，检测重复失败或长时间无进展，并输出统一的升级建议。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `SPEC-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-04/Task-02`
- `Sprint-04/Task-05`

## In Scope

- 检测重复失败和长时间无进展
- 基于已有 failure fingerprint 和计数输出升级或阻塞建议
- 为 `blocked` / `escalated` 决策提供确定性输入
- 补充阈值和升级策略测试

## Out of Scope

- issue 评论和人工交接材料
- 最终的 `needs-human` GitHub 写入
- 启动恢复逻辑
- `Sprint` 级调度策略

## Deliverables

- 无进展检测与升级建议实现
- 阈值、重复失败和升级路径测试

## Acceptance Criteria

- 重复失败和无进展能给出一致信号
- 升级建议可与 orchestrator 当前状态机对接
- 检测逻辑保持确定性，不依赖模糊启发式

## Notes

- 本任务消费 `Task-05` 产出的基础失败信号，不重新定义指纹或计数写入逻辑
