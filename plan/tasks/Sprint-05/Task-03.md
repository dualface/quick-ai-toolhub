# [Sprint-05][Task-03] 实现 task-pr 自动合并接线

## Goal

实现 `Task PR` 的自动合并接线，在满足条件时启用自动合并并同步相关本地状态。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `README.md`
- `TECH-V1.md`
- `TOOLS-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-05/Task-02`
- `Sprint-05/Task-01`

## In Scope

- 在满足条件时启用 `Task PR` 自动合并
- 读取或同步自动合并相关状态
- 将自动合并状态映射到本地 PR 投影
- 补充自动合并接线测试

## Out of Scope

- `Task PR` 创建和更新
- `GitHub Actions` 状态读取
- `Sprint PR` 的人工审查状态

## Deliverables

- Task PR 自动合并接线实现
- 自动合并接线测试

## Acceptance Criteria

- 输出结构符合 `TECH-V1.md`
- 自动合并只在满足条件时启用
- 自动合并状态可被本地 PR 投影稳定反映

## Notes

- 自动合并接线依赖 outbox 执行，不绕开统一 GitHub 写入口
