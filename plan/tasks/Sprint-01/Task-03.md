# [Sprint-01][Task-03] 建立 SQLite/Bun store 基础设施

## Goal

建立基于 `SQLite + Bun` 的数据库访问基础设施，支持打开数据库、执行 schema 初始化和事务封装。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `TECH-V1.md`
- `sql/schema.sql`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-01/Task-01`
- `Sprint-01/Task-02`

## In Scope

- 打开 `SQLite` 数据库连接
- 为每个连接显式开启 `foreign_keys`
- 从 `sql/schema.sql` 初始化数据库
- 封装基础事务入口和共享 store 基类

## Out of Scope

- 具体业务 store 方法
- `task-state-store-tool` 的 7 个操作
- GitHub 同步投影写入逻辑

## Deliverables

- 数据库初始化代码
- 事务辅助代码
- 基础数据库测试

## Acceptance Criteria

- 空数据库可以基于 `sql/schema.sql` 成功初始化
- 新连接会显式开启 `foreign_keys`
- 事务接口可供后续 store 实现直接复用

## Notes

- 数据库结构真源始终是 `sql/schema.sql`
