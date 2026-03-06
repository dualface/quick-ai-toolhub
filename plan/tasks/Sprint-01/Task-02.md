# [Sprint-01][Task-02] 实现 YAML 配置加载与校验

## Goal

定义 `v1` 运行所需的配置结构，支持从 `config/config.yaml` 加载并完成必要校验。

## Reads

- `PROJECT-DEVELOPER-GUIDE.md`
- `TECH-V1.md`
- `plan/SPRINTS-V1.md`

## Dependencies

- `Sprint-01/Task-01`

## In Scope

- 定义配置结构体
- 实现从 `config/config.yaml` 读取配置
- 支持 `CONFIG_FILE` 覆盖默认配置路径
- 对运行所需的必填字段做明确校验

## Out of Scope

- 多层配置覆盖策略
- 远程配置中心
- 密钥管理系统

## Deliverables

- 配置加载与校验代码
- 默认配置文件 `config/config.yaml`
- 覆盖配置路径和校验失败的测试

## Acceptance Criteria

- 默认配置路径可用
- `CONFIG_FILE` 可以覆盖默认路径
- 缺失必填字段时返回明确错误
- 加载后的配置对象可被后续任务直接复用

## Notes

- 配置文件格式统一使用 `YAML`
