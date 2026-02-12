package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
)

var mdRenderer *glamour.TermRenderer

func initRenderer() {
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(100),
	)
	if err == nil {
		mdRenderer = r
	}
}

func renderMarkdown(text string) {
	if mdRenderer == nil || strings.TrimSpace(text) == "" {
		if text != "" {
			fmt.Println(text)
		}
		return
	}

	out, err := mdRenderer.Render(text)
	if err != nil {
		fmt.Println(text)
		return
	}
	fmt.Print(out)
}

func renderContextLine(usage *Usage, maxContext int) {
	if usage == nil {
		return
	}
	total := usage.InputTokens + usage.OutputTokens
	totalK := float64(total) / 1000
	maxK := float64(maxContext) / 1000

	// Dim color
	fmt.Printf("\033[2m── ctx: %.1fk/%.0fk tokens ──\033[0m\n", totalK, maxK)
}

func renderToolCall(name string, args string, blocked bool) {
	if blocked {
		fmt.Printf("\033[33m⚠ %s (blocked in plan mode)\033[0m\n", name)
	} else {
		fmt.Printf("\033[36m▶ %s\033[0m\n", name)
	}
}
