# PROJECT-DEVELOPER-GUIDE

本文件是项目范围的统一开发手册。所有 `Global Leader`、`Task Orchestrator`、`Developer`、`QA`、`Reviewer` 在开始具体任务前都必须先阅读本文件。

## 文档分工

各文档职责如下：

- `README.md`：架构原则、角色分工、流程和状态机约束
- `SPEC-V1.md`：`v1` 行为规格、数据模型、同步规则和边界
- `TECH-V1.md`：技术选型、工具 schema、实现约束
- `TOOLS-V1.md`：工具清单和实现顺序
- `plan/SPRINTS-V1.md`：`Sprint` / `Task` 拆解
- 当前 `Task` brief：该任务的目标、范围、交付物和验收标准
- `sql/schema.sql`：数据库结构真源
- 本文件：项目范围的统一开发规则和工程约束

发生冲突时，按以下原则处理：

- 系统行为、状态机、数据模型以 `README.md`、`SPEC-V1.md`、`TECH-V1.md`、`sql/schema.sql` 为准
- 单个任务的目标、范围、交付物以当前 `Task` brief 为准
- 工程实现习惯、目录约定、验证要求以本文件为准

## V1 固定边界

- 只支持单仓库
- 只支持 `GitHub`
- CI 只支持 `GitHub Actions`
- 只运行单个 `Global Leader`
- 人工主要负责维护任务列表、审查并手动合并 `Sprint PR`

## 实现默认选型

- 主语言使用 `Go`
- 数据库使用 `SQLite`
- 数据库访问使用 `Bun`
- 配置文件统一使用 `YAML`
- 默认配置文件为 `config/config.yaml`
- GitHub 出站操作统一使用 `gh`
- 本地 Git 操作统一使用 `git`
- HTTP 服务使用 `net/http`
- 进程日志使用 `log/slog`，输出 `JSON`

## GitHub 与 Git 约束

- 所有 `gh` 命令必须在目标仓库的 worktree 中执行
- 默认不要求执行 `gh repo set-default`
- 如果命令运行目录不在目标仓库 worktree 中，必须显式传 `-R <owner>/<repo>`
- 本地分支、提交、rebase、push 仍使用 `git`
- 不要直接内嵌 GitHub HTTP API client；统一通过 `gh` 或 `gh api`

## 数据与 schema 约束

- `sql/schema.sql` 是数据库结构真源，不使用 ORM 自动建表替代它
- 每个 SQLite 连接都必须显式开启 `foreign_keys`
- `events` 表是 append-only，不允许更新或删除
- 所有时间字段统一使用 UTC ISO 8601 字符串
- 结构化字段命名统一使用 `snake_case`
- `payload_json`、`request_payload_json`、`*_json` 字段存 JSON 字符串

## 目录约定

建议实现按以下目录组织：

```text
cmd/
  toolhub/
internal/
  leader/
  orchestrator/
  store/
  github/
  git/
  timeline/
sql/
  schema.sql
config/
  config.yaml
plan/
```

## 开发规则

- 优先做小范围、可验证的改动，不做无关重构
- 新增实现必须遵守现有 `SPEC-V1`、`TECH-V1`、`TOOLS-V1`
- 文档内引用仓库文件时使用相对路径，不使用绝对路径
- 不把 task 级内部运行状态回写成大量 GitHub labels 或 issue 评论
- 不擅自扩展 `v1` 范围，例如多仓库、多 leader、非 GitHub CI

## 测试与验证

- 每个 `Task` 至少验证与本次改动直接相关的行为
- 修改数据库或 store 逻辑时，优先补充对应单元测试
- 修改 schema 时，至少验证 `sql/schema.sql` 可以被 `sqlite3` 正常执行
- 如果存在未运行的测试或未验证的风险，必须在结果中明确说明

## Agent 执行输入

执行具体任务时，除本文件外，还必须读取：

- 当前 `Task` brief
- 所属 `Sprint` 上下文
- 该任务直接引用的规格文档和代码文件

如果任务信息不完整，优先回到当前 `Task` brief 和 `SPEC-V1.md` 补全，不自行发明隐含需求。

## 输出要求

Agent 完成工作后，至少应提供：

- 变更摘要
- 主要文件列表
- 已执行的验证
- 未解决风险或待人工确认项
