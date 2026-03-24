# weCodex

weCodex 是一个独立的 Go CLI，用来把微信消息桥接到 Codex ACP 子进程。

当前版本聚焦文本对话 MVP：在微信里发送消息，桥接器把消息转给本地 `codex acp`，再把回复发回微信。

## 当前能力

- 提供 `status`、`login`、`start` 三个命令
- 通过 iLink 二维码登录并本地保存凭证
- 为每个微信用户维护一个本地会话
- 支持微信侧本地命令：`/help`、`/status`、`/new`
- 只允许 `readonly` 权限模式启动 Codex ACP

## 环境要求

- Go 1.24+
- 本机可执行的 Codex CLI
- 可用的微信 iLink 登录环境

## 构建

```bash
go build -o weCodex .
```

也可以直接运行：

```bash
go run . --help
```

## 配置

默认配置文件路径：`~/.weCodex/config.json`

最小可用配置示例：

```json
{
  "codex_command": "codex",
  "codex_args": ["acp"],
  "working_directory": "/absolute/path/to/your/project",
  "permission_mode": "readonly"
}
```

字段说明：

- `codex_command`：Codex 可执行文件名或绝对路径
- `codex_args`：启动 ACP 时传给 Codex 的参数，当前应为 `['acp']`，实际 JSON 写法见上面的示例
- `working_directory`：Codex 执行时使用的工作目录
- `permission_mode`：当前必须是 `readonly`
- `wechat_accounts_dir`：可选，自定义微信凭证目录；未设置时默认写入 `~/.weCodex`
- `log_level`：可选；当前版本可省略

## 使用流程

### 1. 检查静态就绪状态

```bash
./weCodex status
```

这个命令只做静态检查，不会真正启动桥接器。它会检查：

- 配置文件是否存在/可解析
- 登录凭证是否存在
- `codex_command` 是否可解析

当输出中出现 `ready: yes` 时，表示最基本的启动前条件已满足。

### 2. 登录微信 iLink

```bash
./weCodex login
```

执行后会：

1. 拉取二维码
2. 在终端输出二维码内容
3. 轮询登录状态
4. 登录成功后把凭证保存到本地

成功后会输出：

```text
Login succeeded.
```

默认凭证路径：`~/.weCodex/account.json`

如果配置了 `wechat_accounts_dir`，则凭证会保存到该目录下的 `account.json`。

### 3. 启动桥接器

```bash
./weCodex start
```

启动后会在前台运行，并输出：

```text
running in foreground; bridge stays attached to this terminal until interrupted
```

这表示进程会一直占用当前终端，直到你手动中断，例如按 `Ctrl+C`。

## 微信侧命令

桥接器启动后，在微信里可以发送以下本地命令：

- `/help`：查看本地命令帮助
- `/status`：查看桥接器运行状态、ACP 状态、当前是否有活动会话
- `/new`：重置当前用户的本地会话，开始新会话

除以上命令外，其余文本都会作为普通 prompt 转发给 Codex。

## 运行时行为

- 桥接器以**前台模式**运行，不会自行 daemonize
- 当前只支持 **read-only** 权限模型
- 每个微信用户会维持一个本地会话，直到主动 `/new` 或发生错误重置
- 单次 prompt 超时时间为 **120 秒**
- 当前实现一次只处理一条正在执行的请求；上一条请求未结束时，新请求会收到忙碌提示

## 本地文件位置

默认情况下，weCodex 会使用这些路径：

- 配置文件：`~/.weCodex/config.json`
- 登录凭证：`~/.weCodex/account.json`
- 游标文件：`~/.weCodex/ilink_cursor.json`

## 常见问题

### `status` 显示 `ready: no`

按输出逐项排查：

- `config: missing`：先创建 `~/.weCodex/config.json`
- `credentials: missing`：先执行 `weCodex login`
- `codex command: unresolvable`：确认 `codex_command` 可执行且在 PATH 中，或改成绝对路径

### `start` 后为什么终端一直不返回？

这是预期行为。`start` 本来就是前台运行模式，桥接器会持续监听消息，直到你手动中断。

## 项目结构

- `cmd/`：CLI 命令实现（`status` / `login` / `start`）
- `bridge/`：微信消息到 Codex ACP 的桥接逻辑
- `codexacp/`：Codex ACP 客户端与权限判定
- `ilink/`：iLink 登录、凭证、消息收发与监控
- `config/`：配置加载、校验与默认路径逻辑

## 当前边界

当前 README 只描述仓库里已经实现的行为，不包含其他项目中的 channel/plugin 工作流，也不依赖额外图片或示意图。
