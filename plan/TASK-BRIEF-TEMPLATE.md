# TASK-BRIEF-TEMPLATE

每个 `Task brief` 保持短、小、精确，只描述当前任务必须知道的信息，不重复整套项目规格。

```md
# [Sprint-XX][Task-XX] <title>

## Goal

<一句话说明任务目标>

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `plan/SPRINTS-V1.md`
- <本任务直接依赖的规格或代码文件>

## Dependencies

- <前置 task，若无则写 None>

## In Scope

- <本任务必须完成的内容>

## Out of Scope

- <本任务明确不做的内容>

## Deliverables

- <需要提交的代码、文档、测试或产物>

## Acceptance Criteria

- <可验证的完成条件>

## Notes

- <实现注意事项；没有则写 None>
```

补充约束：

- 任务目标、范围、交付物必须与 `plan/SPRINTS-V1.md` 一致
- 通用工程规则不重复抄写，统一引用 `PROJECT-DEVELOPER-GUIDE.md`
- 需要精确契约时，直接引用 `SPEC-V1.md`、`TECH-V1.md`、`TOOLS-V1.md`、`sql/schema.sql`
