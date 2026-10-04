package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"gliss/internal/llm"
)

const maxBashOutput = 30 * 1024 // 输出截断上限 30KB

// BashTool 在工作目录内执行 shell 命令。
type BashTool struct {
	root           string
	DefaultTimeout int // 毫秒
}

func (t *BashTool) Name() string { return "run_bash" }

func (t *BashTool) Definition() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.FunctionDef{
			Name:        "run_bash",
			Description: "在项目工作目录执行 bash 命令（构建、测试、git 等），返回合并的 stdout/stderr。默认 120 秒超时。",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command":         map[string]any{"type": "string", "description": "要执行的命令"},
					"timeout_seconds": map[string]any{"type": "integer", "description": "超时秒数，默认 120"},
				},
				"required": []string{"command"},
			},
		},
	}
}

func (t *BashTool) Execute(ctx context.Context, argsJSON string) string {
	var args struct {
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := parseArgs(argsJSON, &args); err != nil {
		return errText(err)
	}
	if args.Command == "" {
		return "错误: command 不能为空"
	}

	timeout := time.Duration(t.DefaultTimeout) * time.Millisecond
	if args.TimeoutSeconds > 0 {
		timeout = time.Duration(args.TimeoutSeconds) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(ctx, "bash", "-c", args.Command)
	cmd.Dir = t.root
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	runErr := cmd.Run()
	elapsed := time.Since(start).Round(time.Millisecond)

	out := buf.Bytes()
	truncated := false
	if len(out) > maxBashOutput {
		out = out[:maxBashOutput]
		truncated = true
	}

	var sb bytes.Buffer
	if len(out) > 0 {
		sb.Write(out)
		if truncated {
			sb.WriteString("\n...（输出已截断至 30KB）")
		}
		sb.WriteString("\n")
	}

	if ctx.Err() == context.DeadlineExceeded {
		return sb.String() + fmt.Sprintf("超时: 命令在 %s 后被终止", timeout)
	}
	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); ok {
			return sb.String() + fmt.Sprintf("[退出码 %d, 耗时 %s]", ee.ExitCode(), elapsed)
		}
		return errText(runErr)
	}
	return sb.String() + fmt.Sprintf("[成功, 耗时 %s]", elapsed)
}
