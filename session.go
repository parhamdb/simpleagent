package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Session struct {
	ID         string    `json:"id"`
	CreatedAt  string    `json:"created_at"`
	UpdatedAt  string    `json:"updated_at"`
	Provider   string    `json:"provider"`
	Model      string    `json:"model"`
	Messages   []Message `json:"messages"`
	Summary    string    `json:"summary"`
	TokensUsed int       `json:"tokens_used"`
}

type SessionIndex struct {
	Sessions []SessionEntry `json:"sessions"`
}

type SessionEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	Summary   string `json:"summary"`
}

func sessionsDir() string {
	return filepath.Join(agentDir, "sessions")
}

func ensureSessionsDir() {
	os.MkdirAll(sessionsDir(), 0755)
}

func NewSession(provider, model string) *Session {
	now := time.Now().Format(time.RFC3339)
	return &Session{
		ID:        uuid.New().String(),
		CreatedAt: now,
		UpdatedAt: now,
		Provider:  provider,
		Model:     model,
	}
}

func (s *Session) Save() error {
	ensureSessionsDir()
	dir := sessionsDir()
	s.UpdatedAt = time.Now().Format(time.RFC3339)

	// Generate summary from first user message if empty
	if s.Summary == "" {
		for _, m := range s.Messages {
			if m.Role == "user" && m.Content != "" {
				s.Summary = truncate(m.Content, 60)
				break
			}
		}
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(dir, s.ID+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}

	updateSessionIndex(s)
	os.WriteFile(filepath.Join(dir, "last_session"), []byte(s.ID), 0644)
	return nil
}

func LoadSession(id string) (*Session, error) {
	path := filepath.Join(sessionsDir(), id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func loadSessionByIDOrName(idOrName string) (*Session, error) {
	if s, err := LoadSession(idOrName); err == nil {
		return s, nil
	}

	idx := loadSessionIndex()
	for _, e := range idx.Sessions {
		if e.Name == idOrName {
			return LoadSession(e.ID)
		}
	}

	return nil, fmt.Errorf("session not found: %s", idOrName)
}

func loadLastSession() *Session {
	data, err := os.ReadFile(filepath.Join(sessionsDir(), "last_session"))
	if err != nil {
		return nil
	}
	id := strings.TrimSpace(string(data))
	s, err := LoadSession(id)
	if err != nil {
		return nil
	}
	return s
}

func loadSessionIndex() SessionIndex {
	var idx SessionIndex
	data, err := os.ReadFile(filepath.Join(sessionsDir(), "sessions.json"))
	if err != nil {
		return idx
	}
	json.Unmarshal(data, &idx)
	return idx
}

func updateSessionIndex(s *Session) {
	idx := loadSessionIndex()
	dir := sessionsDir()

	found := false
	for i, e := range idx.Sessions {
		if e.ID == s.ID {
			idx.Sessions[i].Summary = s.Summary
			idx.Sessions[i].CreatedAt = s.CreatedAt
			found = true
			break
		}
	}
	if !found {
		idx.Sessions = append(idx.Sessions, SessionEntry{
			ID:        s.ID,
			CreatedAt: s.CreatedAt,
			Summary:   s.Summary,
		})
	}

	data, _ := json.MarshalIndent(idx, "", "  ")
	os.WriteFile(filepath.Join(dir, "sessions.json"), data, 0644)
}

func renameSession(id, name string) {
	idx := loadSessionIndex()
	dir := sessionsDir()
	for i, e := range idx.Sessions {
		if e.ID == id {
			idx.Sessions[i].Name = name
			break
		}
	}
	data, _ := json.MarshalIndent(idx, "", "  ")
	os.WriteFile(filepath.Join(dir, "sessions.json"), data, 0644)
}

func listAllSessions() {
	idx := loadSessionIndex()
	if len(idx.Sessions) == 0 {
		fmt.Println("No sessions found.")
		return
	}

	sort.Slice(idx.Sessions, func(i, j int) bool {
		return idx.Sessions[i].CreatedAt > idx.Sessions[j].CreatedAt
	})

	for _, e := range idx.Sessions {
		name := e.Name
		if name == "" {
			name = e.ID[:8]
		}
		age := formatAge(e.CreatedAt)
		fmt.Printf("  %-20s (%s)  %q\n", name, age, e.Summary)
	}
}

func sessionPicker() *Session {
	idx := loadSessionIndex()

	fmt.Printf("simpleagent v%s\n\n", version)

	if len(idx.Sessions) == 0 {
		fmt.Println("Starting new session.")
		fmt.Println()
		return nil
	}

	sort.Slice(idx.Sessions, func(i, j int) bool {
		return idx.Sessions[i].CreatedAt > idx.Sessions[j].CreatedAt
	})

	limit := 5
	if len(idx.Sessions) < limit {
		limit = len(idx.Sessions)
	}
	recent := idx.Sessions[:limit]

	fmt.Println("Recent sessions:")
	for i, e := range recent {
		name := e.Name
		if name == "" {
			name = e.ID[:8]
		}
		age := formatAge(e.CreatedAt)
		summary := e.Summary
		if summary == "" {
			summary = "(empty)"
		}
		fmt.Printf("  %d. %-20s (%s)  %q\n", i+1, name, age, summary)
	}
	fmt.Printf("  0. New Session\n\n")

	fmt.Printf("Pick [0-%d]: ", limit)

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return nil
	}
	choice := strings.TrimSpace(scanner.Text())

	if choice == "" || choice == "0" {
		return nil
	}

	n, err := strconv.Atoi(choice)
	if err != nil || n < 1 || n > limit {
		return nil
	}

	s, err := LoadSession(recent[n-1].ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading session: %v\n", err)
		return nil
	}
	return s
}

func formatAge(rfc3339 string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return "?"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func truncate(s string, n int) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	if len(s) > n {
		return s[:n-3] + "..."
	}
	return s
}
