// Package tools 实现 coding agent 的内置工具集：
// 文件读写、代码搜索与 shell 执行，所有路径被限制在工作目录内。
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"gliss/internal/llm"
)

// Tool 是一个可被 LLM 调用的工具。
type Tool interface {
	// Name 是工具名（即 function name）。
	Name() string
	// Description 返回 OpenAI function 定义（含参数 JSON Schema）。
	Definition() llm.ToolDef
	// Execute 执行工具。argsJSON 是 LLM 给出的参数 JSON 字符串。
	// 结果文本始终返回（错误也作为文本返回，便于模型自我纠正）。
	Execute(ctx context.Context, argsJSON string) string
}

// Registry 持有全部工具，按名分发。
type Registry struct {
	tools   map[string]Tool
	ordered []Tool
	// Root 是路径安全锚点：所有工具的文件操作不得逃逸出该目录。
	Root string
}

// NewRegistry 创建工具集。root 必须是绝对路径。
func NewRegistry(root string, bashTimeoutMs int) *Registry {
	if bashTimeoutMs <= 0 {
		bashTimeoutMs = 120_000
	}
	r := &Registry{tools: map[string]Tool{}, Root: root}
	r.register(&ReadFileTool{root: root})
	r.register(&WriteFileTool{root: root})
	r.register(&EditFileTool{root: root})
	r.register(&ListDirTool{root: root})
	r.register(&GlobTool{root: root})
	r.register(&GrepTool{root: root})
	r.register(&BashTool{root: root, DefaultTimeout: bashTimeoutMs})
	return r
}

func (r *Registry) register(t Tool) {
	r.tools[t.Name()] = t
	r.ordered = append(r.ordered, t)
}

// All 按注册顺序返回全部工具。
func (r *Registry) All() []Tool { return r.ordered }

// Get 按名查找工具。
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Definitions 导出全部工具定义。
func (r *Registry) Definitions() []llm.ToolDef {
	defs := make([]llm.ToolDef, 0, len(r.ordered))
	for _, t := range r.ordered {
		defs = append(defs, t.Definition())
	}
	return defs
}

// Execute 分发执行；未知工具返回错误文本。
func (r *Registry) Execute(ctx context.Context, name, argsJSON string) string {
	t, ok := r.tools[name]
	if !ok {
		return fmt.Sprintf("错误: 未知工具 %q", name)
	}
	return t.Execute(ctx, argsJSON)
}

// ---- 路径安全 ----

// resolvePath 把用户/模型给出的路径规范化为绝对路径，并确保不逃逸出 root。
func resolvePath(root, p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("路径为空")
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("解析路径失败: %w", err)
	}
	// 清洗 ../ 序列后再校验相对关系，防止逃逸。
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", fmt.Errorf("路径 %q 超出工作目录范围", p)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("路径 %q 超出工作目录范围", p)
	}
	return abs, nil
}

// parseArgs 解码参数 JSON 到目标结构，解码失败返回错误。
func parseArgs(argsJSON string, dst any) error {
	if strings.TrimSpace(argsJSON) == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(argsJSON), dst); err != nil {
		return fmt.Errorf("参数 JSON 非法: %w", err)
	}
	return nil
}

// errText 把错误转为返回给模型的文本。
func errText(err error) string {
	return "错误: " + err.Error()
}
