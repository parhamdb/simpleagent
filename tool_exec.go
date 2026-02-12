package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
)

// ringBuffer is a fixed-size circular byte buffer.
type ringBuffer struct {
	buf    []byte
	size   int
	pos    int
	full   bool
	unread int // bytes written since last Read()
	mu     sync.Mutex
}

func newRingBuffer(size int) *ringBuffer {
	return &ringBuffer{
		buf:  make([]byte, size),
		size: size,
	}
}

func (rb *ringBuffer) Write(p []byte) (int, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	n := len(p)
	rb.unread += n

	for _, b := range p {
		rb.buf[rb.pos] = b
		rb.pos = (rb.pos + 1) % rb.size
		if rb.pos == 0 {
			rb.full = true
		}
	}
	return n, nil
}

// ReadUnread returns bytes written since the last call to ReadUnread.
func (rb *ringBuffer) ReadUnread() string {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.unread == 0 {
		return ""
	}

	// How many bytes are actually available in the buffer
	avail := rb.pos
	if rb.full {
		avail = rb.size
	}

	// We want min(unread, avail) bytes
	want := rb.unread
	if want > avail {
		want = avail
	}

	result := make([]byte, want)
	start := rb.pos - want
	if start < 0 {
		start += rb.size
	}
	for i := 0; i < want; i++ {
		result[i] = rb.buf[(start+i)%rb.size]
	}

	rb.unread = 0
	return string(result)
}

// ReadAll returns all bytes currently in the buffer.
func (rb *ringBuffer) ReadAll() string {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if !rb.full && rb.pos == 0 {
		return ""
	}

	var avail int
	if rb.full {
		avail = rb.size
	} else {
		avail = rb.pos
	}

	result := make([]byte, avail)
	start := 0
	if rb.full {
		start = rb.pos
	}
	for i := 0; i < avail; i++ {
		result[i] = rb.buf[(start+i)%rb.size]
	}

	rb.unread = 0
	return string(result)
}

// ManagedProcess represents a background process managed by the agent.
type ManagedProcess struct {
	ID      string
	Name    string
	Cmd     *exec.Cmd
	Stdin   io.WriteCloser
	Stdout  *ringBuffer
	Stderr  *ringBuffer
	Started time.Time
	Done    bool
	ExitErr error
	mu      sync.Mutex
}

var processes = struct {
	sync.Mutex
	m map[string]*ManagedProcess
}{m: make(map[string]*ManagedProcess)}

func registerExecTools(r *ToolRegistry) {
	r.Register(ToolDef{
		Name:        "bash",
		Description: "Run a shell command and BLOCK until it finishes. Returns stdout and stderr. Do NOT use for servers, watchers, or anything that runs indefinitely — use start_process instead.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string", "description": "Shell command to execute"},
				"timeout": map[string]any{"type": "integer", "description": "Timeout in seconds (default 120)"},
				"stdin":   map[string]any{"type": "string", "description": "String to pipe to command's stdin"},
				"workdir": map[string]any{"type": "string", "description": "Working directory for the command"},
				"env":     map[string]any{"type": "object", "description": "Extra environment variables (key-value pairs)"},
			},
			"required": []string{"command"},
		},
	}, toolBash, true)

	r.Register(ToolDef{
		Name:        "start_process",
		Description: "Start a background process (servers, watchers, long-running commands). Returns a handle ID. Use read_output to check output, write_stdin to send input, kill_process to stop.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string", "description": "Shell command to execute"},
				"workdir": map[string]any{"type": "string", "description": "Working directory"},
				"env":     map[string]any{"type": "object", "description": "Extra environment variables"},
			},
			"required": []string{"command"},
		},
	}, toolStartProcess, true)

	r.Register(ToolDef{
		Name:        "write_stdin",
		Description: "Send input to a running process's stdin.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":   map[string]any{"type": "string", "description": "Process handle ID"},
				"text": map[string]any{"type": "string", "description": "Text to write (newline appended if missing)"},
			},
			"required": []string{"id", "text"},
		},
	}, toolWriteStdin, true)

	r.Register(ToolDef{
		Name:        "read_output",
		Description: "Read buffered stdout/stderr from a managed process (non-blocking).",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "Process handle ID"},
			},
			"required": []string{"id"},
		},
	}, toolReadOutput, false)

	r.Register(ToolDef{
		Name:        "kill_process",
		Description: "Kill a managed process by handle ID. Sends SIGTERM, then SIGKILL after 3 seconds.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "Process handle ID"},
			},
			"required": []string{"id"},
		},
	}, toolKillProcess, true)

	r.Register(ToolDef{
		Name:        "list_processes",
		Description: "List all managed background processes with status.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, toolListProcesses, false)
}

var bashTimeout = 120 // overridden from config

func toolBash(args json.RawMessage) (string, error) {
	var params struct {
		Command string            `json:"command"`
		Timeout int               `json:"timeout"`
		Stdin   string            `json:"stdin"`
		Workdir string            `json:"workdir"`
		Env     map[string]string `json:"env"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	timeout := bashTimeout
	if params.Timeout > 0 {
		timeout = params.Timeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", params.Command)

	if params.Workdir != "" {
		cmd.Dir = params.Workdir
	}

	if len(params.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range params.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	if params.Stdin != "" {
		cmd.Stdin = strings.NewReader(params.Stdin)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	var result string
	if stdout.Len() > 0 {
		result += stdout.String()
	}
	if stderr.Len() > 0 {
		if result != "" {
			result += "\n"
		}
		result += "STDERR:\n" + stderr.String()
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result += fmt.Sprintf("\n[timed out after %ds]", timeout)
		} else {
			result += fmt.Sprintf("\n[exit: %v]", err)
		}
	}

	if result == "" {
		result = "(no output)"
	}

	const maxOutput = 50000
	if len(result) > maxOutput {
		result = result[:maxOutput] + "\n... [truncated]"
	}

	return result, nil
}

func toolStartProcess(args json.RawMessage) (string, error) {
	var params struct {
		Command string            `json:"command"`
		Workdir string            `json:"workdir"`
		Env     map[string]string `json:"env"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	cmd := exec.Command("sh", "-c", params.Command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if params.Workdir != "" {
		cmd.Dir = params.Workdir
	}
	if len(params.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range params.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	const bufSize = 64 * 1024 // 64KB ring buffers

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Sprintf("error: %v", err), nil
	}

	stdoutBuf := newRingBuffer(bufSize)
	stderrBuf := newRingBuffer(bufSize)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("error starting process: %v", err), nil
	}

	id := uuid.New().String()[:8]
	name := params.Command
	if len(name) > 60 {
		name = name[:60] + "..."
	}

	mp := &ManagedProcess{
		ID:      id,
		Name:    name,
		Cmd:     cmd,
		Stdin:   stdin,
		Stdout:  stdoutBuf,
		Stderr:  stderrBuf,
		Started: time.Now(),
	}

	// Monitor process exit in background
	go func() {
		exitErr := cmd.Wait()
		mp.mu.Lock()
		mp.Done = true
		mp.ExitErr = exitErr
		mp.mu.Unlock()
	}()

	processes.Lock()
	processes.m[id] = mp
	processes.Unlock()

	return fmt.Sprintf("started process %s (pid %d): %s", id, cmd.Process.Pid, name), nil
}

func toolWriteStdin(args json.RawMessage) (string, error) {
	var params struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	processes.Lock()
	mp, ok := processes.m[params.ID]
	processes.Unlock()
	if !ok {
		return fmt.Sprintf("error: no process with id %s", params.ID), nil
	}

	mp.mu.Lock()
	done := mp.Done
	mp.mu.Unlock()
	if done {
		return "error: process has already exited", nil
	}

	text := params.Text
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}

	if _, err := io.WriteString(mp.Stdin, text); err != nil {
		return fmt.Sprintf("error writing to stdin: %v", err), nil
	}
	return fmt.Sprintf("wrote %d bytes to process %s stdin", len(text), params.ID), nil
}

func toolReadOutput(args json.RawMessage) (string, error) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	processes.Lock()
	mp, ok := processes.m[params.ID]
	processes.Unlock()
	if !ok {
		return fmt.Sprintf("error: no process with id %s", params.ID), nil
	}

	stdout := mp.Stdout.ReadUnread()
	stderr := mp.Stderr.ReadUnread()

	mp.mu.Lock()
	done := mp.Done
	exitErr := mp.ExitErr
	mp.mu.Unlock()

	var sb strings.Builder
	if stdout != "" {
		sb.WriteString(stdout)
	}
	if stderr != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("STDERR:\n")
		sb.WriteString(stderr)
	}

	if done {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		if exitErr != nil {
			sb.WriteString(fmt.Sprintf("[process exited: %v]", exitErr))
		} else {
			sb.WriteString("[process exited: 0]")
		}
	}

	if sb.Len() == 0 {
		return "(no new output)", nil
	}
	return sb.String(), nil
}

func toolKillProcess(args json.RawMessage) (string, error) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	processes.Lock()
	mp, ok := processes.m[params.ID]
	processes.Unlock()
	if !ok {
		return fmt.Sprintf("error: no process with id %s", params.ID), nil
	}

	mp.mu.Lock()
	done := mp.Done
	mp.mu.Unlock()
	if done {
		return fmt.Sprintf("process %s already exited", params.ID), nil
	}

	// Send SIGTERM to process group
	pgid, err := syscall.Getpgid(mp.Cmd.Process.Pid)
	if err == nil {
		syscall.Kill(-pgid, syscall.SIGTERM)
	} else {
		mp.Cmd.Process.Signal(syscall.SIGTERM)
	}

	// Wait up to 3 seconds for graceful exit
	done = false
	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		mp.mu.Lock()
		done = mp.Done
		mp.mu.Unlock()
		if done {
			break
		}
	}

	if !done {
		if pgid, err := syscall.Getpgid(mp.Cmd.Process.Pid); err == nil {
			syscall.Kill(-pgid, syscall.SIGKILL)
		} else {
			mp.Cmd.Process.Kill()
		}
		return fmt.Sprintf("killed process %s (SIGKILL)", params.ID), nil
	}

	return fmt.Sprintf("terminated process %s", params.ID), nil
}

func toolListProcesses(args json.RawMessage) (string, error) {
	processes.Lock()
	defer processes.Unlock()

	if len(processes.m) == 0 {
		return "(no managed processes)", nil
	}

	var sb strings.Builder
	for id, mp := range processes.m {
		mp.mu.Lock()
		done := mp.Done
		exitErr := mp.ExitErr
		mp.mu.Unlock()

		status := "running"
		if done {
			if exitErr != nil {
				status = fmt.Sprintf("exited (%v)", exitErr)
			} else {
				status = "exited (0)"
			}
		}

		uptime := time.Since(mp.Started).Truncate(time.Second)
		fmt.Fprintf(&sb, "%s  %s  %s  uptime=%s\n", id, status, mp.Name, uptime)
	}
	return sb.String(), nil
}
