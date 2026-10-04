package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gliss/internal/llm"
)

const maxFileBytes = 2 * 1024 * 1024 // 单文件读取上限 2MB

// ReadFileTool 读取文件内容，带行号输出，支持 offset/limit 分页。
type ReadFileTool struct{ root string }

func (t *ReadFileTool) Name() string { return "read_file" }

func (t *ReadFileTool) Definition() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.FunctionDef{
			Name:        "read_file",
			Description: "读取文本文件内容，输出带行号。大文件请用 offset/limit 分段读取。",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":   map[string]any{"type": "string", "description": "文件路径（相对工作目录或绝对路径）"},
					"offset": map[string]any{"type": "integer", "description": "起始行号（从 1 开始），默认 1"},
					"limit":  map[string]any{"type": "integer", "description": "读取的最大行数，默认 2000"},
				},
				"required": []string{"path"},
			},
		},
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, argsJSON string) string {
	var args struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := parseArgs(argsJSON, &args); err != nil {
		return errText(err)
	}
	abs, err := resolvePath(t.root, args.Path)
	if err != nil {
		return errText(err)
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return errText(err)
	}
	if len(data) > maxFileBytes {
		return fmt.Sprintf("错误: 文件超过 %d MB 读取上限，请用 grep/glob 定位后用 offset/limit 分段读取", maxFileBytes/1024/1024)
	}

	lines := strings.Split(string(data), "\n")
	if args.Offset < 1 {
		args.Offset = 1
	}
	if args.Limit <= 0 {
		args.Limit = 2000
	}

	var sb strings.Builder
	end := args.Offset - 1 + args.Limit
	if end > len(lines) {
		end = len(lines)
	}
	for i := args.Offset - 1; i < end; i++ {
		fmt.Fprintf(&sb, "%6d\t%s\n", i+1, lines[i])
	}
	if end < len(lines) {
		fmt.Fprintf(&sb, "...（共 %d 行，已省略后续 %d 行，可用 offset 继续读取）", len(lines), len(lines)-end)
	}
	out := sb.String()
	// 结果本身也有长度限制，防止撑爆上下文。
	if len(out) > 30*1024 {
		out = out[:30*1024] + "\n...（输出已截断，请用 offset/limit 分段读取）"
	}
	return out
}

// WriteFileTool 覆盖写入文件，自动创建父目录。
type WriteFileTool struct{ root string }

func (t *WriteFileTool) Name() string { return "write_file" }

func (t *WriteFileTool) Definition() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.FunctionDef{
			Name:        "write_file",
			Description: "将文本内容写入文件（整文件覆盖）。新建文件或全量重写时使用；小改动优先用 edit_file。",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "文件路径"},
					"content": map[string]any{"type": "string", "description": "完整文件内容"},
				},
				"required": []string{"path", "content"},
			},
		},
	}
}

func (t *WriteFileTool) Execute(ctx context.Context, argsJSON string) string {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := parseArgs(argsJSON, &args); err != nil {
		return errText(err)
	}
	abs, err := resolvePath(t.root, args.Path)
	if err != nil {
		return errText(err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return errText(err)
	}
	if err := os.WriteFile(abs, []byte(args.Content), 0o644); err != nil {
		return errText(err)
	}
	return fmt.Sprintf("已写入 %s（%d 字节）", relDisplay(t.root, abs), len(args.Content))
}

// EditFileTool 精确字符串替换。
type EditFileTool struct{ root string }

func (t *EditFileTool) Name() string { return "edit_file" }

func (t *EditFileTool) Definition() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.FunctionDef{
			Name:        "edit_file",
			Description: "对文件做精确字符串替换。old_string 必须与文件内容完全一致且唯一；多处匹配时需提供更长上下文使其唯一，或设 replace_all。",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":        map[string]any{"type": "string", "description": "文件路径"},
					"old_string":  map[string]any{"type": "string", "description": "被替换的原文（含足够上下文以保证唯一）"},
					"new_string":  map[string]any{"type": "string", "description": "替换后的文本"},
					"replace_all": map[string]any{"type": "boolean", "description": "替换全部匹配，默认 false"},
				},
				"required": []string{"path", "old_string", "new_string"},
			},
		},
	}
}

func (t *EditFileTool) Execute(ctx context.Context, argsJSON string) string {
	var args struct {
		Path       string `json:"path"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := parseArgs(argsJSON, &args); err != nil {
		return errText(err)
	}
	abs, err := resolvePath(t.root, args.Path)
	if err != nil {
		return errText(err)
	}
	if args.OldString == "" {
		return "错误: old_string 不能为空"
	}
	if args.OldString == args.NewString {
		return "错误: old_string 与 new_string 相同"
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return errText(err)
	}
	content := string(data)
	count := strings.Count(content, args.OldString)
	switch {
	case count == 0:
		// 给出近似提示帮助模型纠正。
		return "错误: 未找到 old_string。请先用 read_file 查看文件当前内容，确认字符串完全一致（包括缩进与空行）。"
	case count > 1 && !args.ReplaceAll:
		return fmt.Sprintf("错误: old_string 出现 %d 处，不唯一。请扩大上下文范围，或设置 replace_all=true。", count)
	}

	updated := strings.Replace(content, args.OldString, args.NewString, 1)
	if args.ReplaceAll {
		updated = strings.ReplaceAll(content, args.OldString, args.NewString)
	}
	if err := os.WriteFile(abs, []byte(updated), 0o644); err != nil {
		return errText(err)
	}
	return fmt.Sprintf("已编辑 %s（替换 %d 处）", relDisplay(t.root, abs), count)
}

// ListDirTool 列出目录内容。
type ListDirTool struct{ root string }

func (t *ListDirTool) Name() string { return "list_dir" }

func (t *ListDirTool) Definition() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.FunctionDef{
			Name:        "list_dir",
			Description: "列出目录的直接子项，标注类型（目录/文件）与大小。",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "目录路径，默认工作目录"},
				},
			},
		},
	}
}

func (t *ListDirTool) Execute(ctx context.Context, argsJSON string) string {
	var args struct {
		Path string `json:"path"`
	}
	if err := parseArgs(argsJSON, &args); err != nil {
		return errText(err)
	}
	if args.Path == "" {
		args.Path = "."
	}
	abs, err := resolvePath(t.root, args.Path)
	if err != nil {
		return errText(err)
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return errText(err)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s/\n", relDisplay(t.root, abs))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			fmt.Fprintf(&sb, "  %s/\n", name)
			continue
		}
		info, err := e.Info()
		if err != nil {
			fmt.Fprintf(&sb, "  %s\n", name)
			continue
		}
		fmt.Fprintf(&sb, "  %s (%s)\n", name, humanSize(info.Size()))
	}
	out := sb.String()
	if len(out) > 30*1024 {
		out = out[:30*1024] + "\n...（输出已截断）"
	}
	return out
}

func relDisplay(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return abs
	}
	return rel
}

func humanSize(n int64) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.1fMB", float64(n)/1024/1024)
	case n >= 1024:
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	default:
		return fmt.Sprintf("%dB", n)
	}
}
