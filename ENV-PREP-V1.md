# ENV-PREP-V1

本文件列出本项目在本机运行前必须完成的环境准备工作。

目标只有一个：让 `toolhub`、`gh`、`git`、`codex`、`Go` 相关验证在本机稳定执行，不把环境缺口混进代码问题里。

## 必须安装的工具

以下命令必须可直接从 `PATH` 调用：

- `go`
- `git`
- `gh`
- `codex`
- `sqlite3`
- `golangci-lint`

当前项目会直接依赖它们做这些事情：

- `go`：编译、测试、`go vet`
- `git`：本地分支、worktree、提交、push
- `gh`：GitHub issue / PR / Actions 操作
- `codex`：`run-agent-tool` 的唯一 runner
- `sqlite3`：验证 `sql/schema.sql`
- `golangci-lint`：QA lint 验证

## 必须完成的认证

### GitHub CLI

- 必须完成 `gh` 登录
- 必须对目标仓库有足够权限

检查：

```bash
gh auth status
```

### Codex CLI

- 必须完成 `codex` 登录

检查：

```bash
codex login status
```

## 必须满足的运行条件

### 1. 仓库 worktree

- 所有 `gh` 命令都必须在目标仓库的 worktree 中执行
- `toolhub run-task` 的 `--workdir` 必须指向目标仓库 worktree

### 2. Go 临时目录可写

必须允许 Go 创建临时构建目录和缓存目录。

当前实现会为 `developer` / `qa` 自动准备 repo-local 目录：

- `.toolhub/runtime/tmp`
- `.toolhub/runtime/go-build`
- `.toolhub/runtime/go-cache`

如果手工执行 `go build` / `go vet`，建议也显式使用这些目录：

```bash
export TMPDIR="$PWD/.toolhub/runtime/tmp"
export TMP="$TMPDIR"
export TEMP="$TMPDIR"
export GOTMPDIR="$PWD/.toolhub/runtime/go-build"
export GOCACHE="$PWD/.toolhub/runtime/go-cache"
```

### 3. Reviewer 只读约束

- `reviewer` 必须保持 worktree 只读
- runner 自己需要写入的临时文件必须写到 worktree 外的显式附加目录

这条不需要手工配置，但本机环境不能阻止 `codex --add-dir` 使用额外可写目录。

### 4. 网络访问

至少要允许：

- `gh` 访问 GitHub
- `codex` 访问其后端服务
- `go install` / `go` 拉取依赖（如首次安装或首次构建需要）

## 推荐的本机检查命令

在仓库根目录执行：

```bash
which go
go version

which git
git --version

which gh
gh --version
gh auth status

which codex
codex --version
codex login status

which sqlite3
sqlite3 --version

which golangci-lint
golangci-lint --version
```

## 进入开发前的最小验证

```bash
go test ./...
go build ./...
go vet ./...
golangci-lint run
sqlite3 /tmp/toolhub_schema_check.db < sql/schema.sql
```

如果其中某条命令因为环境原因失败，先修环境，不要直接进入 `QA`。

## 当前确认的外部缺口

此前 QA 结果里明确暴露过的环境缺口有：

- 缺少 `golangci-lint`
- Go 无法在受限环境里使用默认 `/tmp/go-build*`

其中：

- `golangci-lint` 现在已经安装
- Go 临时目录问题已经通过 repo-local runtime 目录在 `run-agent-tool` 中处理，但手工执行命令时仍应按上面的环境变量方式运行

## 不属于环境准备的问题

下面这些不应归类为“缺工具”：

- 代码本身的 schema 契约错误
- 失败路径没有返回结构化结果
- reviewer 权限模型实现错误
- 并发 run 目录冲突

这些属于实现问题，应回到代码修复。
