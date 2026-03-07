# [Sprint-04][Task-05] 实现失败指纹与重试计数

## Goal

实现 failure fingerprint 和各阶段失败计数逻辑，为后续无进展检测和升级策略提供稳定输入。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `SPEC-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-04/Task-02`
- `Sprint-04/Task-04`

## In Scope

- 生成 failure fingerprint
- 统计 `attempt_total`、`qa_fail_count`、`review_fail_count`、`ci_fail_count`
- 输出稳定、可复用的失败统计信号

## Out of Scope

- 重复失败判定策略
- 长时间无进展检测
- 升级或阻塞建议输出
- issue 评论和人工交接材料
- 最终的 `needs-human` GitHub 写入
- 恢复逻辑

## Deliverables

- failure fingerprint 与计数实现
- 指纹生成和计数测试

## Acceptance Criteria

- 对同类失败能生成稳定指纹
- 相关计数可与 `tasks` 表中的字段对应
- 后续任务可以直接复用这些信号做升级判断

## Notes

- 本任务只沉淀“基础信号”，不在这里定义最终升级策略
