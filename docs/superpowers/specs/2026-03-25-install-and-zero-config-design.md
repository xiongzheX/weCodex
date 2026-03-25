# wecodex 安装与零配置启动设计

**目标**

让用户在安装后尽量直接使用以下命令，无需手动创建配置文件：

```bash
wecodex login
wecodex status
wecodex start
```

默认行为应使用 CLI backend，而不是要求用户先手动编写 `~/.weCodex/config.json`。

---

## 1. 背景

当前项目已经支持两种后端：

- `acp`
- `cli`

但现有使用流程仍然要求用户先手动准备配置文件，否则 `status`、`login`、`start` 无法顺滑工作。对首次用户来说，这一步是额外负担，也和“安装后直接可用”的目标冲突。

用户已经确认本次目标是：

1. 同时支持 `go install` 和安装脚本两种安装方式
2. 文档主推 `wecodex` 命令名
3. 兼容 `weCodex` 旧命令名，但以安装脚本提供该兼容入口为主
4. 首次运行时自动生成默认配置
5. 默认 backend 使用 `cli`
6. 默认工作目录使用运行命令时的当前终端目录
7. `login` / `status` / `start` 都统一使用同一套首次 bootstrap 行为

---

## 2. 用户体验目标

### 2.1 推荐安装方式

为两类用户提供两条路径：

#### 开发者路径

```bash
go install github.com/xiongzhe/weCodex/cmd/wecodex@latest
```

这个安装路径产出的规范命令名为：

```bash
wecodex
```

安装后可直接运行：

```bash
wecodex status
wecodex login
wecodex start
```

#### 普通用户路径

提供一个一键安装脚本，负责从 GitHub Releases 下载预编译二进制，并把它安装到 PATH 中的可执行目录。本次范围里该脚本只支持 Darwin/macOS。

文档中主推：

- 已安装 Go 的用户使用 `go install`
- 普通用户使用安装脚本

### 2.2 首次运行体验

当用户第一次执行以下任一命令时：

- `wecodex login`
- `wecodex status`
- `wecodex start`

如果 `~/.weCodex/config.json` 不存在，程序自动写入默认配置，然后继续执行当前命令，而不是要求用户先手动创建配置文件。

首次 bootstrap 之后，命令是否成功仍取决于原本的前置条件：

- `status`：继续输出静态就绪状态；如果 `codex` 不在 PATH 中，仍会显示 `codex command: unresolvable`
- `login`：继续进入登录流程
- `start`：继续尝试启动；如果 `codex` 不在 PATH 中或后端启动失败，仍然明确报错

也就是说，“安装后直接可用”在本次设计里的精确定义是：

- 用户不需要手动创建配置文件
- 命令可以直接运行到各自原本的检查/执行阶段
- 缺少 `codex`、未登录等前置条件时，仍按现有语义报错或提示，而不是额外做魔法修复

### 2.3 默认配置

自动生成的默认配置内容为：

```json
{
  "backend_type": "cli",
  "codex_command": "codex",
  "codex_args": [],
  "working_directory": "<运行命令时的当前目录>",
  "working_directory_mode": "auto",
  "permission_mode": "readonly"
}
```

其中：

- `backend_type` 固定为 `cli`
- `codex_command` 固定为 `codex`
- `codex_args` 为空数组
- `working_directory` 初始值使用运行命令时的当前工作目录
- `working_directory_mode` 初始值为 `auto`
- `permission_mode` 固定为 `readonly`

其中 `working_directory_mode` 的语义明确如下：

- `auto`：命令每次运行时都使用当前终端目录，并把该值同步回配置文件
- `manual`：命令不再自动改写 `working_directory`，完全使用配置文件中的持久值

legacy 配置或用户已存在配置如果没有该字段，一律按 `manual` 处理，以避免无提示改写老用户配置。

---

## 3. 核心设计

### 3.1 引入 bootstrap 配置层

在配置加载逻辑上增加一个很小的 bootstrap 层，用于处理“配置缺失时自动创建默认配置”的场景。

建议增加一个类似下面职责的入口：

- 读取配置
- 如果配置不存在，则构造默认配置
- 确保 `~/.weCodex/` 目录存在
- 以原子写入方式保存默认配置到 `~/.weCodex/config.json`
- 再返回该配置给调用方

这个入口应被 `login`、`status`、`start` 共用，避免每个命令自己复制一套逻辑。

并且这个入口需要显式定义以下边界：

- 如果配置目录不存在，自动创建目录
- 如果首次运行时两个进程并发创建配置文件，最终结果应保持为一个合法配置文件，不出现损坏文件；若两个进程当前目录不同，则采用先成功写入配置文件的那个目录，后续进程读取现成配置继续执行
- 如果写入过程中失败，不留下半写入的损坏配置
- 如果配置文件存在但为空，视为非法配置，报错，不自动覆盖

### 3.2 默认工作目录来源

默认工作目录不应写死为 `$HOME` 或仓库路径，而应使用命令启动时的当前目录。

原因：

- 这正是用户运行 `wecodex start` 时希望 Codex 工作的项目目录
- 零配置情况下最符合直觉
- 避免用户安装后还要修改工作目录才能真正使用

因此 bootstrap 层需要在运行时获取当前目录，并将其写入首次生成的配置。

进一步地，目录行为只由 `working_directory_mode` 决定：

- 若 `working_directory_mode=auto`，则每次运行命令时都以当前终端目录作为 `working_directory`
- 若 `working_directory_mode=manual`，则始终使用配置文件中的持久值
- 程序**不推断**用户是否“手动改过配置”
- 用户若希望固定目录，需要显式把 `working_directory_mode` 改为 `manual`

### 3.3 命令一致性

以下命令都应走同一套自动 bootstrap 逻辑：

- `login`
- `status`
- `start`

这样行为一致：

- 如果没有配置文件，都会自动补齐
- 如果有合法配置，都正常读取
- 如果配置已存在但损坏，都明确报错

---

## 4. 兼容与边界

### 4.1 只处理“配置缺失”

自动生成默认配置只在以下条件下触发：

- `config.Load()` 返回 `os.ErrNotExist`

以下情况**不自动修复**：

- JSON 解析失败
- 字段校验失败
- 权限错误
- 读取失败

这些情况应继续向用户显式报错，避免静默覆盖用户已有配置。

### 4.2 不覆盖已有配置

如果 `~/.weCodex/config.json` 已经存在：

- 不自动改成 `cli`
- 对 legacy 配置或普通用户配置，不自动改工作目录
- 不自动迁移字段

唯一允许自动更新 `working_directory` 的情况，是配置中显式写明 `working_directory_mode=auto`。

保持现有配置优先，避免破坏已有用户环境。

### 4.3 首次 bootstrap 的提示策略

三条命令统一使用同一套 bootstrap 逻辑，但首次创建默认配置时的用户可见行为也需要统一。

建议行为：

- 当默认配置被首次创建时，向终端输出固定文案：`default config created: ~/.weCodex/config.json (backend: cli)`
- 这个提示在 `login` / `status` / `start` 中保持一致
- 若配置已存在，则不输出该提示

这样用户能知道程序替他做了什么，同时不会引入交互式打断。

### 4.4 不增加额外交互

本次范围内不引入：

- 交互式首次引导
- wizard
- 自动项目探测
- 自动登录
- 自动修复 PATH
- 包管理器发布流程

目标是只解决“安装后直接可用”和“零手动配置”的主问题。

---

## 5. 命令命名策略

### 5.1 主推命令名

文档、帮助信息、示例统一主推：

```bash
wecodex
```

### 5.2 兼容命令名

本次将 `weCodex` 兼容入口收敛为一个明确要求：

- 规范命令名是 `wecodex`
- 安装脚本必须额外提供可实际执行的 `weCodex` 兼容入口
- 原始 `go install` 路径只保证产出规范命令名 `wecodex`，不要求同时创建两个可执行文件

设计原则：

- 用户面对的公开命令优先简单、全小写
- 不强行打断现有 CamelCase 使用方式

---

## 6. 实现落点

本次设计预计会涉及以下区域：

### `config/`

- 增加“缺配置时生成默认配置”的入口
- 增加默认配置构造逻辑
- 增加获取当前工作目录并写入默认配置的能力
- 增加相关测试

### `cmd/login.go`

- 改为通过 bootstrap 配置入口读取配置
- 保持现有登录逻辑不变

### `cmd/status.go`

- 改为通过 bootstrap 配置入口读取配置
- 在配置缺失时不再只报 missing，而是先创建默认配置再继续静态检查

### `cmd/start.go`

- 改为通过 bootstrap 配置入口读取配置
- 使用自动生成的默认 CLI backend 配置继续启动

### `cmd/root.go`

- 更新根命令展示名与帮助文案，主推 `wecodex`

### 安装脚本

- 新增一键安装脚本，属于本次范围
- 该脚本本次只支持 Darwin/macOS
- 脚本从 GitHub Releases 下载 Darwin 预编译二进制
- 脚本负责把主命令安装为 `wecodex`
- 脚本还必须同时提供 `weCodex` 兼容入口

### 新的 Go 安装入口

- 新增一个可通过 `go install github.com/xiongzhe/weCodex/cmd/wecodex@latest` 安装的入口
- 该入口的目标是稳定产出小写命令名 `wecodex`

### `README.md`

- 改写安装章节
- 增加 `go install` 与安装脚本说明
- 更新配置章节，说明默认配置会自动生成
- 更新使用示例为 `wecodex status` / `wecodex login` / `wecodex start`

---

## 7. 测试策略

应优先用测试覆盖以下行为：

### 配置层

- 无配置文件时，bootstrap 会生成默认配置
- 默认配置为 `cli`
- 默认工作目录来自当前目录
- 默认配置写入 `working_directory_mode=auto`
- `working_directory_mode=auto` 时，后续命令运行会继续跟随当前终端目录
- `working_directory_mode=manual` 时，后续命令运行不自动更新目录
- legacy 配置缺少该字段时，按 `manual` 处理
- 已有合法配置时，不会被覆盖
- 已有非法配置时，会报错而不是覆盖
- 首次写入使用原子写入，不留下损坏配置

### 命令层

- `status` 在无配置时会自动生成配置并继续输出状态
- `login` 在无配置时会自动生成配置并继续
- `start` 在无配置时会自动生成配置并继续走 CLI backend
- 三条命令在首次创建默认配置时都输出统一提示：`default config created: ~/.weCodex/config.json (backend: cli)`
- `start` 在缺少 `codex` 或 backend 启动失败时，仍按既有语义失败

### 安装与命名

- 安装脚本属于本次交付范围
- 通过 `go install github.com/xiongzhe/weCodex/cmd/wecodex@latest` 安装后，用户可实际执行 `wecodex`
- 通过安装脚本安装后，用户可实际执行 `wecodex` 与 `weCodex`
- 根命令帮助文案主推 `wecodex`
- README 示例与新行为一致

---

## 8. 成功标准

满足以下条件即可认为本次设计目标达成：

1. 在没有 `~/.weCodex/config.json` 的前提下，执行 `wecodex status` 会自动创建默认配置文件，并继续输出状态结果
2. 在没有 `~/.weCodex/config.json` 的前提下，执行 `wecodex login` 会自动创建默认配置文件，并继续进入登录流程
3. 在没有 `~/.weCodex/config.json` 的前提下，执行 `wecodex start` 会自动创建默认配置文件，并继续按 CLI backend 尝试启动
4. 自动生成的默认配置中，`backend_type=cli`、`codex_command=codex`、`codex_args=[]`、`working_directory_mode=auto`、`permission_mode=readonly`
5. 当 `working_directory_mode=auto` 时，`working_directory` 会跟随运行命令时的当前目录；当 `working_directory_mode=manual` 时，不自动更新
6. legacy 配置缺少 `working_directory_mode` 时，按 `manual` 处理
7. 三条命令在首次创建默认配置时都输出固定提示：`default config created: ~/.weCodex/config.json (backend: cli)`
8. 已存在合法配置不会被自动覆盖
9. 已存在但损坏或非法的配置会明确报错，不会被自动覆盖
10. 安装脚本属于本次交付范围，且通过安装脚本安装后用户可实际执行 `wecodex` 与 `weCodex`
11. 通过 `go install github.com/xiongzhe/weCodex/cmd/wecodex@latest` 安装后，用户可实际执行 `wecodex`

---

## 9. 不在本次范围

以下内容暂不纳入本次工作：

- 自动判断 `codex` 是否支持 ACP 并动态切换 backend
- 自动选择最优安装路径
- Homebrew / apt / npm 等包管理器发布
- GUI/交互式初始化界面
- 自动生成多项目配置

这些都可以在后续作为独立设计继续扩展。
