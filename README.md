# weCodex

weCodex 是一个独立的 Go CLI，用来把微信消息桥接到本地 Codex 运行时。

当前版本聚焦文本对话 MVP：在微信里发送消息，桥接器把消息转给本地 Codex 后端，再把回复发回微信。当前支持两种后端：`codex acp` 和 `codex exec`。

## 当前能力

- 提供 `status`、`login`、`start` 三个命令
- 通过 iLink 二维码登录并本地保存凭证
- 为每个微信用户维护当前选中的 Codex CLI 线程
- 支持微信侧本地命令：`/help`、`/status`、`/new`、`/list`、`/use N`
- 支持 `acp` 和 `cli` 两种 Codex 后端
- 只允许 `readonly` 权限模式启动

## 环境要求

- Go 1.24+
- 本机可执行的 Codex CLI
- 可用的微信 iLink 登录环境

## 安装

### 方式 1：Go install

```bash
go install github.com/xiongzheX/weCodex/cmd/wecodex@latest
```

如果安装后执行 `wecodex` 提示 `command not found`，通常是 Go bin 目录不在 `PATH`。可按下面处理：

```bash
# 立即生效（当前终端）
export PATH="$(go env GOPATH)/bin:$PATH"

# 验证
which wecodex
wecodex --help
```

zsh 用户可持久化到 `~/.zshrc`：

```bash
echo 'export PATH="$(go env GOPATH)/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

### 方式 2：Darwin 安装脚本（macOS）

仓库内脚本路径：`scripts/install.sh`

```bash
bash scripts/install.sh
```

安装脚本行为：

- 仅支持 Darwin（macOS）
- 自动识别 `arm64` / `amd64`
- 从 GitHub Releases 下载最新资产：`wecodex-darwin-${arch}.tar.gz`
- 在当前 `PATH` 中选择**第一个可写且已存在**的目录作为安装目录
- 安装 `wecodex` 可执行文件
- 同时安装 `weCodex` 兼容别名（兼容旧命令名）

## 构建

```bash
go build -o wecodex .
```

也可以直接运行：

```bash
go run . --help
```

## 配置

默认配置文件路径：`~/.weCodex/config.json`

`wecodex status`、`wecodex login`、`wecodex start` 在首次运行时，如果配置文件不存在，会自动创建默认配置。

默认配置 JSON（精确字段）如下：

```json
{
  "backend_type": "cli",
  "codex_command": "codex",
  "codex_args": [],
  "working_directory": "<启动命令时的当前终端目录>",
  "working_directory_mode": "auto",
  "permission_mode": "readonly"
}
```

字段说明：

- `backend_type`：可选；`acp` 或 `cli`，未设置时默认按 `acp` 处理
- `codex_command`：Codex 可执行文件名或绝对路径
- `codex_args`：传给 Codex 的附加参数；`acp` 后端必须包含启动 ACP 所需参数，`cli` 后端可以为空
- `working_directory`：Codex 执行时使用的工作目录
- `working_directory_mode`：`auto` 或 `manual`
  - `auto`：每次命令启动时，自动同步为当前终端目录
  - `manual`：保持配置文件中的 `working_directory` 不变
- `permission_mode`：当前必须是 `readonly`
- `wechat_accounts_dir`：可选，自定义微信凭证目录；未设置时默认写入 `~/.weCodex`
- `log_level`：可选；当前版本可省略

推荐在 `codex acp` 可用时使用 `acp` 后端；如果本机 Codex CLI 不支持 `acp`，改用 `cli` 后端。

## 使用流程

### 1. 检查静态就绪状态

```bash
wecodex status
```

这个命令只做静态检查，不会真正启动桥接器。它会检查：

- 配置文件是否存在/可解析
- 登录凭证是否存在
- `codex_command` 是否可解析

当输出中出现 `ready: yes` 时，表示最基本的启动前条件已满足。

### 2. 登录微信 iLink

```bash
wecodex login
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
wecodex start
```

启动后会在前台运行，并输出：

```text
running in foreground; bridge stays attached to this terminal until interrupted
```

这表示进程会一直占用当前终端，直到你手动中断，例如按 `Ctrl+C`。

## 微信侧命令

桥接器启动后，在微信里可以发送以下本地命令：

- `/help`：查看本地命令帮助
- `/status`：查看桥接器运行状态、后端状态、当前是否有活动会话
- `/new`：创建一个新的 Codex CLI 线程并切换过去
- `/list`：列出当前项目目录下的 Codex CLI 线程
- `/use N`：切换到 `/list` 显示的第 N 个线程

除以上命令外，其余文本都会作为普通 prompt 转发给 Codex。

## 运行时行为

- 桥接器以**前台模式**运行，不会自行 daemonize
- 当前只支持 **read-only** 权限模型
- ACP 后端当前不支持线程列表与切换能力
- CLI 后端会读取并继续当前项目目录下的 Codex CLI 线程；`/new`、`/list` 和 `/use N` 都直接作用于 Codex CLI 线程
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

- `config: missing`：首次运行会自动创建 `~/.weCodex/config.json`，请先重新执行 `wecodex status` 或 `wecodex login`
- `credentials: missing`：先执行 `wecodex login`
- `codex command: unresolvable`：确认 `codex_command` 可执行且在 PATH 中，或改成绝对路径

### `start` 后为什么终端一直不返回？

这是预期行为。`start` 本来就是前台运行模式，桥接器会持续监听消息，直到你手动中断。

## 项目结构

- `cmd/`：CLI 命令实现（`status` / `login` / `start`）
- `bridge/`：微信消息到 Codex 后端的桥接逻辑
- `backend/`：后端抽象、ACP 适配器和 `codex exec` CLI 后端
- `codexacp/`：Codex ACP 客户端与权限判定
- `ilink/`：iLink 登录、凭证、消息收发与监控
- `config/`：配置加载、校验与默认路径逻辑

## 当前边界

当前 README 只描述仓库里已经实现的行为，不包含其他项目中的 channel/plugin 工作流，也不依赖额外图片或示意图。
