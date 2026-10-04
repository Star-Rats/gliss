// Package config 加载 Gliss 的运行配置。
// 优先级：命令行 flags > 环境变量 > 配置文件 > 默认值。
package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Config 是 Gliss 的全部运行时配置。
type Config struct {
	// BaseURL 是 OpenAI 兼容端点的根地址，例如 https://api.openai.com/v1
	BaseURL string `json:"base_url"`
	// APIKey 是鉴权用的 Bearer token。
	APIKey string `json:"api_key"`
	// Model 是模型名，例如 glm-4.7、deepseek-chat。
	Model string `json:"model"`
	// MaxTurns 是单次提交内 LLM 调用次数上限，防止工具循环失控。
	MaxTurns int `json:"max_turns"`
	// BashTimeout 是 run_bash 工具的默认超时。
	BashTimeout time.Duration `json:"bash_timeout_seconds"`
	// WorkDir 是工具可访问的根目录（路径逃逸防护的锚点）。
	WorkDir string `json:"-"`
}

// Default 返回内置默认配置。
func Default() *Config {
	return &Config{
		BaseURL:     "https://api.openai.com/v1",
		Model:       "gpt-4o",
		MaxTurns:    40,
		BashTimeout: 120 * time.Second,
	}
}

// ConfigPath 返回配置文件路径：$GLISS_CONFIG，否则 ~/.config/gliss/config.json。
func ConfigPath() (string, error) {
	if p := os.Getenv("GLISS_CONFIG"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("获取用户主目录失败: %w", err)
	}
	return filepath.Join(home, ".config", "gliss", "config.json"), nil
}

// Load 依次合并配置文件、环境变量与 flags，并做最终校验。
func Load(args []string) (*Config, error) {
	cfg := Default()

	if p, err := ConfigPath(); err == nil {
		if data, err := os.ReadFile(p); err == nil {
			if err := json.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("解析配置文件 %s 失败: %w", p, err)
			}
		}
	} else {
		return nil, err
	}

	if v := os.Getenv("GLISS_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv("OPENAI_BASE_URL"); v != "" && os.Getenv("GLISS_BASE_URL") == "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv("GLISS_API_KEY"); v != "" {
		cfg.APIKey = v
	}
	if v := os.Getenv("OPENAI_API_KEY"); v != "" && os.Getenv("GLISS_API_KEY") == "" {
		cfg.APIKey = v
	}
	if v := os.Getenv("GLISS_MODEL"); v != "" {
		cfg.Model = v
	}

	fs := flag.NewFlagSet("gliss", flag.ContinueOnError)
	fs.StringVar(&cfg.BaseURL, "base-url", cfg.BaseURL, "OpenAI 兼容 API 根地址")
	fs.StringVar(&cfg.APIKey, "api-key", cfg.APIKey, "API 密钥")
	fs.StringVar(&cfg.Model, "model", cfg.Model, "模型名")
	fs.IntVar(&cfg.MaxTurns, "max-turns", cfg.MaxTurns, "单次提交的 LLM 调用轮数上限")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if extra := fs.Args(); len(extra) > 0 {
		return nil, fmt.Errorf("无法识别的参数: %v", extra)
	}

	if cfg.APIKey == "" {
		return nil, fmt.Errorf("未配置 API Key：请设置环境变量 GLISS_API_KEY，或使用 -api-key，或在 %s 中配置 api_key", mustConfigPath())
	}
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 40
	}
	if cfg.BashTimeout <= 0 {
		cfg.BashTimeout = 120 * time.Second
	}

	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("获取工作目录失败: %w", err)
	}
	cfg.WorkDir = wd

	return cfg, nil
}

func mustConfigPath() string {
	p, err := ConfigPath()
	if err != nil {
		return "~/.config/gliss/config.json"
	}
	return p
}
