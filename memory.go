package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func loadMemory() string {
	data, err := os.ReadFile(filepath.Join(agentDir, "AGENT.md"))
	if err != nil {
		return ""
	}
	return "## Agent Memory\n" + string(data) + "\n"
}

func appendMemory(text string) error {
	os.MkdirAll(agentDir, 0755)
	path := filepath.Join(agentDir, "AGENT.md")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	date := time.Now().Format("2006-01-02")
	_, err = fmt.Fprintf(f, "\n## %s\n- %s\n", date, text)
	return err
}
