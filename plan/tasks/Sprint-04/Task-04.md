# [Sprint-04][Task-04] 实现失败预算与无进展检测

## Goal

实现失败指纹、重试计数和无进展检测逻辑，为 `blocked` / `escalated` 决策提供统一信号。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `SPEC-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-04/Task-02`
- `Sprint-04/Task-03`

## In Scope

- 生成 failure fingerprint
- 统计 `attempt_total`、`qa_fail_count`、`review_fail_count`、`ci_fail_count`
- 检测重复失败和长时间无进展
- 输出升级或阻塞建议

## Out of Scope

- issue 评论和人工交接材料
- 最终的 `needs-human` GitHub 写入
- 恢复逻辑

## Deliverables

- 失败预算和无进展检测实现
- 指纹生成和阈值测试

## Acceptance Criteria

- 对同类失败能生成稳定指纹
- 重复失败和无进展能给出一致信号
- 相关计数可与 `tasks` 表中的字段对应

## Notes

- 检测逻辑先保持确定性，避免引入模糊启发式
