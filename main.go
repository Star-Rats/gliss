// Gliss 是一个 Go + Bubble Tea 实现的终端 coding agent。
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"gliss/internal/agent"
	"gliss/internal/config"
	"gliss/internal/llm"
	"gliss/internal/tools"
	"gliss/internal/ui"
)

func main() {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "gliss:", err)
		os.Exit(1)
	}

	client := llm.NewClient(cfg.BaseURL, cfg.APIKey, cfg.Model)
	registry := tools.NewRegistry(cfg.WorkDir, int(cfg.BashTimeout.Milliseconds()))
	ag := agent.New(client, registry, agent.SystemPrompt(cfg.WorkDir), cfg.MaxTurns)
	defer ag.Stop()

	program := tea.NewProgram(ui.New(ag, cfg),
		tea.WithAltScreen(),
		tea.WithOutput(os.Stderr),
	)

	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "gliss: 界面启动失败:", err)
		os.Exit(1)
	}
}
