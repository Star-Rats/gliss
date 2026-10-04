package tools

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"gliss/internal/llm"
)

const (
	maxGlobResults = 500
	maxGrepResults = 200
)

// GlobTool 按 glob 模式匹配文件路径，支持 **。
type GlobTool struct{ root string }

func (t *GlobTool) Name() string { return "glob" }

func (t *GlobTool) Definition() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.FunctionDef{
			Name:        "glob",
			Description: "按 glob 模式查找文件路径，支持 ** 递归（如 **/*.go）。返回最多 500 条相对路径。",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{"type": "string", "description": "glob 模式，如 *.go、src/**/*_test.go"},
				},
				"required": []string{"pattern"},
			},
		},
	}
}

func (t *GlobTool) Execute(ctx context.Context, argsJSON string) string {
	var args struct {
		Pattern string `json:"pattern"`
	}
	if err := parseArgs(argsJSON, &args); err != nil {
		return errText(err)
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return "错误: pattern 不能为空"
	}
	// fs.FS 要求合法的 slash 路径，去掉开头的 ./
	pattern := strings.TrimPrefix(filepath.ToSlash(args.Pattern), "./")
	if pattern == "" {
		return "错误: pattern 不能为空"
	}

	matches, err := doublestar.Glob(os.DirFS(t.root), pattern)
	if err != nil {
		return errText(fmt.Errorf("glob 执行失败: %w", err))
	}
	if len(matches) == 0 {
		return "未找到匹配文件"
	}
	sort.Strings(matches)
	if len(matches) > maxGlobResults {
		matches = matches[:maxGlobResults]
	}
	out := strings.Join(matches, "\n")
	if len(matches) == maxGlobResults {
		out += fmt.Sprintf("\n...（结果已达 %d 条上限）", maxGlobResults)
	}
	return out
}

// GrepTool 在文件内容中做正则搜索。
type GrepTool struct{ root string }

func (t *GrepTool) Name() string { return "grep" }

func (t *GrepTool) Definition() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.FunctionDef{
			Name:        "grep",
			Description: "在文件内容中搜索正则表达式，输出 文件:行号:匹配行。自动跳过 .git、node_modules 与二进制文件。",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{"type": "string", "description": "Go 正则表达式"},
					"path":    map[string]any{"type": "string", "description": "搜索起始目录或单个文件，默认工作目录"},
					"include": map[string]any{"type": "string", "description": "仅搜索文件名匹配该 glob 的文件，如 *.go"},
				},
				"required": []string{"pattern"},
			},
		},
	}
}

func (t *GrepTool) Execute(ctx context.Context, argsJSON string) string {
	var args struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Include string `json:"include"`
	}
	if err := parseArgs(argsJSON, &args); err != nil {
		return errText(err)
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return "错误: pattern 不能为空"
	}
	re, err := regexp.Compile(args.Pattern)
	if err != nil {
		return errText(fmt.Errorf("正则非法: %w", err))
	}
	if args.Path == "" {
		args.Path = "."
	}
	abs, err := resolvePath(t.root, args.Path)
	if err != nil {
		return errText(err)
	}

	singleFile := false
	if info, err := os.Stat(abs); err == nil && !info.IsDir() {
		singleFile = true
	}

	var results []string
	total, fileCount, scanned := 0, 0, 0
	walkErr := filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 无权限等错误直接跳过
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == ".venv" {
				return fs.SkipDir
			}
			return nil
		}
		if !singleFile && args.Include != "" {
			rel := filepath.ToSlash(pathRelativeTo(t.root, path))
			if !doublestar.MatchUnvalidated(filepath.ToSlash(args.Include), rel) &&
				!doublestar.MatchUnvalidated(filepath.ToSlash(args.Include), filepath.Base(path)) {
				return nil
			}
		}
		scanned++
		lines, err := grepFile(path, re)
		if err != nil {
			return nil
		}
		if len(lines) == 0 {
			return nil
		}
		fileCount++
		for _, l := range lines {
			total++
			if len(results) < maxGrepResults {
				results = append(results, fmt.Sprintf("%s:%d:%s", pathRelativeTo(t.root, path), l.line, trimLine(l.text)))
			}
		}
		return nil
	})
	if walkErr != nil && ctx.Err() == nil {
		return errText(walkErr)
	}

	if len(results) == 0 {
		return fmt.Sprintf("未找到匹配（扫描 %d 个文件）", scanned)
	}
	out := strings.Join(results, "\n")
	if total > len(results) {
		out += fmt.Sprintf("\n...（共 %d 处匹配于 %d 个文件，仅显示前 %d 处）", total, fileCount, len(results))
	}
	return out
}

type grepLine struct {
	line int
	text string
}

func grepFile(path string, re *regexp.Regexp) ([]grepLine, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if isBinary(data) {
		return nil, nil
	}
	var lines []grepLine
	for i, line := range strings.Split(string(data), "\n") {
		if re.MatchString(line) {
			lines = append(lines, grepLine{line: i + 1, text: line})
		}
	}
	return lines, nil
}

func isBinary(data []byte) bool {
	n := len(data)
	if n > 8000 {
		n = 8000
	}
	for _, b := range data[:n] {
		if b == 0 {
			return true
		}
	}
	return false
}

func pathRelativeTo(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return abs
	}
	return rel
}

func trimLine(s string) string {
	s = strings.TrimRight(s, "\r")
	if len([]rune(s)) > 240 {
		return string([]rune(s)[:240]) + "..."
	}
	return s
}
