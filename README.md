<div align="center">

# Gliss

**一个用 Go + Bubble Tea 从零实现的终端 coding agent**

LLM 流式对话 · 工具调用 · TUI 交互全部内置,不依赖任何官方 SDK

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Bubble Tea](https://img.shields.io/badge/Bubble_Tea-TUI-FF69B4)](https://github.com/charmbracelet/bubbletea)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](https://github.com/Star-Rats/gliss/pulls)

简体中文 | [English](README_EN.md)

</div>

```
█▀▀▀ █ █ █▀▀ █▀▀    ◆ gliss
█▄▄▀ █ █ ▀▀█ ▀▀█    glm-4.7 ● 就绪
▀▄▄▀ ▀ ▀ ▀▀▀ ▀▀▀    ~/code/jmy/Gliss  ↑1.2k ↓800

❯ 给 util.Add 补一个乘法函数并跑测试
◆  我先看一下现有代码…
✓ read_file path=internal/util/util.go →    12 lines
✓ edit_file path=… old=<1 字符> new=<23 字符>
✓ run_bash command="go test ./internal/util/" → …
◆  已完成:新增 Mul 函数并通过 3 个测试。
── 回合结束:4 次 LLM 调用,3 次工具调用 ──
> 输入任务,/help 查看命令,Enter 发送…
```

## ✨ 亮点

- 🔌 **任意 OpenAI 兼容端点**:智谱 GLM / DeepSeek / Kimi / OpenAI,改一个环境变量就切换,运行中还能 `/model` 热切换
- 📡 **手写 SSE 流式解析**:按 index 累积 `tool_calls` 分片,不依赖任何官方 SDK,一个 `main.go` + 五个 internal 包就是全部
- 🛠️ **7 个内置工具**:读写文件、精确编辑、glob/grep 搜索、执行命令——并带**路径逃逸防护**(解析为绝对路径后必须落在工作目录内)与命令超时
- ⏸️ **随时可中断**:`Esc` 通过 `context.WithCancel` 贯穿 LLM 请求与 bash 子进程,说停就停
- 🎛️ **单回合轮数上限**(`-max-turns`,默认 40):防工具循环失控烧 token
- 🖥️ **为终端而生的交互**:渐变 ASCII 横幅、可滚动历史、多行输入、斜杠命令菜单(`↑↓` 选择 / `tab` 补全)、`ctrl+t` 展开工具调用详情、glamour 渲染 Markdown、token 用量统计

## 🚀 快速开始

```bash
# 构建(需要 Go 1.22+)
go build -o gliss .

# 配置任意 OpenAI 兼容端点
export GLISS_API_KEY=sk-xxx
export GLISS_BASE_URL=https://open.bigmodel.cn/api/paas/v4   # 智谱 GLM
export GLISS_MODEL=glm-4.7

./gliss
```

常见端点示例:

| 服务商 | GLISS_BASE_URL | 模型示例 |
|---|---|---|
| 智谱 GLM | `https://open.bigmodel.cn/api/paas/v4` | `glm-4.7` |
| DeepSeek | `https://api.deepseek.com/v1` | `deepseek-chat` |
| Moonshot Kimi | `https://api.moonshot.cn/v1` | `kimi-k2-0905-preview` |
| OpenAI | `https://api.openai.com/v1` | `gpt-4o` |

## ⚙️ 配置

优先级:命令行 flags > 环境变量 > 配置文件 > 默认值。

```bash
gliss -base-url https://api.deepseek.com/v1 -model deepseek-chat -api-key sk-xxx
```

| 环境变量 | flag | 说明 |
|---|---|---|
| `GLISS_API_KEY` / `OPENAI_API_KEY` | `-api-key` | API 密钥(必填) |
| `GLISS_BASE_URL` / `OPENAI_BASE_URL` | `-base-url` | API 根地址,默认 `https://api.openai.com/v1` |
| `GLISS_MODEL` | `-model` | 模型名,默认 `gpt-4o` |
| | `-max-turns` | 单回合 LLM 调用轮数上限(防工具循环失控),默认 40 |

配置文件 `~/.config/gliss/config.json`(可用 `$GLISS_CONFIG` 覆盖路径):

```json
{
  "base_url": "https://open.bigmodel.cn/api/paas/v4",
  "api_key": "sk-xxx",
  "model": "glm-4.7",
  "max_turns": 40,
  "bash_timeout_seconds": 120
}
```

## ⌨️ 快捷键

| 按键 | 功能 |
|---|---|
| `Enter` | 发送输入 |
| `Shift+Enter` | 输入框内换行 |
| `Esc` | 中断正在生成的回合;空闲时清空输入框 |
| `ctrl+t` | 展开 / 折叠全部工具调用的参数与结果详情 |
| `PgUp` / `PgDn`(`Home` / `End`) | 滚动历史区 |
| `ctrl+c` | 退出 |

## ⌘ 斜杠命令

在输入框输入 `/` 会弹出命令候选菜单,继续输入即过滤,`↑↓` 选择、`tab` 补全、`enter` 执行、`esc` 关闭:

| 命令 | 功能 |
|---|---|
| `/help` | 显示命令与快捷键帮助 |
| `/clear`(`/new`) | 清空会话历史与用量统计,开始新会话 |
| `/model [名称]` | 查看当前模型,或运行中切换模型(如 `/model glm-4.7`) |
| `/usage` | 查看会话累计 token 用量与调用次数 |
| `/tools` | 列出全部内置工具及说明 |
| `/exit`(`/quit`) | 退出 |

未知的 `/` 开头输入(如询问 `/etc/hosts` 相关问题)会作为普通消息发送给模型。

## 🛠️ 内置工具

| 工具 | 说明 |
|---|---|
| `read_file` | 读文件,带行号,支持 `offset` / `limit` 分段 |
| `write_file` | 全量写文件,自动创建父目录 |
| `edit_file` | 精确字符串替换;不唯一时报错,支持 `replace_all` |
| `list_dir` | 列目录 |
| `glob` | 文件名匹配,支持 `**` 递归 |
| `grep` | 内容正则搜索,输出 `文件:行号:行`,跳过 `.git` / 二进制 |
| `run_bash` | 执行 shell 命令(默认 120s 超时,输出截断 30KB) |

安全约束:所有文件路径解析为绝对路径后必须落在工作目录内(`..` 逃逸会被拒绝);`run_bash` 强制超时。

## 🏗️ 架构

```
main.go                 入口:配置加载 → 组件装配 → tea.Program
internal/config         配置(flags > 环境变量 > JSON 文件)
internal/llm            OpenAI 兼容客户端:/chat/completions + SSE 流式解析,
                        按 index 累积 tool_calls 分片
internal/tools          工具接口 + 7 个实现 + 注册表(路径逃逸防护)
internal/agent          会话循环 goroutine:prompt → LLM 流式 → 工具执行 → 回填 → 循环;
                        对 UI 通过事件通道输出(Token/ToolStart/ToolEnd/TurnEnd/…)
internal/ui             Bubble Tea 界面:渐变 ASCII 横幅 / 可滚动历史 / 多行输入 / 斜杠命令 / 快捷键;
                        waitForAgentEvent Cmd 泵事件,流式直出、完成后 glamour 渲染 markdown
```

并发模型:agent 在独立 goroutine 中运行,`Submit()` 提交输入、`Events()` 输出事件;UI 用 Bubble Tea 标准的 Cmd 事件泵桥接两者。`Esc` 中断通过 `context.WithCancel` 贯穿 LLM HTTP 请求与 bash 命令。

## ✅ 测试

```bash
go test ./...        # SSE 解析、工具集、agent 循环、UI 冒烟(含 httptest 模拟服务器)
go test -race ./...
```

## License

[MIT](LICENSE)
