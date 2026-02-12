package main

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// readLine reads a line of input with raw mode support for Shift+Tab detection.
// Returns the input string and whether the user toggled mode (via Shift+Tab).
// On EOF (Ctrl+D), returns "", false with err set.
// Falls back to simple line reading if raw mode is unavailable.
func (a *Agent) readLine() (string, error) {
	fd := int(os.Stdin.Fd())

	if !term.IsTerminal(fd) {
		return a.readLineSimple()
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return a.readLineSimple()
	}
	defer term.Restore(fd, oldState)

	var buf []byte
	esc := make([]byte, 0, 4) // accumulate escape sequences

	for {
		b := make([]byte, 1)
		n, err := os.Stdin.Read(b)
		if err != nil || n == 0 {
			// Restore before returning
			term.Restore(fd, oldState)
			return "", fmt.Errorf("EOF")
		}

		ch := b[0]

		// If we're in an escape sequence
		if len(esc) > 0 {
			esc = append(esc, ch)

			// ESC [ Z = Shift+Tab
			if len(esc) == 3 && esc[0] == 0x1b && esc[1] == '[' && esc[2] == 'Z' {
				esc = esc[:0]
				a.toggleMode()
				// Reprint the prompt on a new line
				fmt.Print("\r\033[K" + a.prompt())
				// Reprint current buffer
				fmt.Print(string(buf))
				continue
			}

			// ESC [ <other> — other escape sequences, just discard
			if len(esc) >= 3 {
				esc = esc[:0]
				continue
			}

			// ESC followed by something other than [
			if len(esc) == 2 && esc[1] != '[' {
				esc = esc[:0]
				continue
			}

			continue
		}

		switch ch {
		case 0x1b: // ESC - start of escape sequence
			esc = append(esc, ch)

		case '\r', '\n': // Enter
			fmt.Print("\r\n")
			term.Restore(fd, oldState)
			return string(buf), nil

		case 0x03: // Ctrl+C
			fmt.Print("^C\r\n")
			term.Restore(fd, oldState)
			os.Exit(0)

		case 0x04: // Ctrl+D
			if len(buf) == 0 {
				fmt.Print("\r\n")
				term.Restore(fd, oldState)
				return "", fmt.Errorf("EOF")
			}

		case 0x7f, 0x08: // Backspace / Delete
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				fmt.Print("\b \b")
			}

		case '\t': // Regular tab — insert spaces or ignore
			// ignore tabs in input

		default:
			if ch >= 0x20 { // printable
				buf = append(buf, ch)
				fmt.Print(string(ch))
			}
		}
	}
}

func (a *Agent) readLineSimple() (string, error) {
	var buf []byte
	b := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(b)
		if err != nil || n == 0 {
			return "", fmt.Errorf("EOF")
		}
		if b[0] == '\n' {
			return string(buf), nil
		}
		if b[0] == '\r' {
			continue
		}
		buf = append(buf, b[0])
	}
}

func (a *Agent) toggleMode() {
	if a.mode == ModePlan {
		a.mode = ModeAction
		fmt.Print("\r\033[K\033[33mSwitched to ACTION mode.\033[0m\n")
	} else {
		a.mode = ModePlan
		fmt.Print("\r\033[K\033[36mSwitched to PLAN mode.\033[0m\n")
	}
}
