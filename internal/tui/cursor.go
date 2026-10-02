package tui

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

const (
	nativeCursorPositionMarker = "\x1b_gh-elm-cursor-position\x1b\\"
	nativeCursorCommandPrefix  = "\x1b_gh-elm-native-cursor:"
	nativeCursorCommandSuffix  = "\x1b\\"
)

type nativeCursorWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

type nativeCursorTerminalWriter struct {
	*nativeCursorWriter
	term.File
}

func (w *nativeCursorTerminalWriter) Write(p []byte) (int, error) {
	return w.nativeCursorWriter.Write(p)
}

// NativeCursorOutput adapts Bubble Tea output so forms can use the terminal's
// native cursor without changing the user's configured cursor shape or blink mode.
func NativeCursorOutput(writer io.Writer) io.Writer {
	cursorWriter := &nativeCursorWriter{writer: writer}
	if file, ok := writer.(term.File); ok {
		return &nativeCursorTerminalWriter{nativeCursorWriter: cursorWriter, File: file}
	}
	return cursorWriter
}

func (w *nativeCursorWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	output, command := stripNativeCursorCommands(p)
	if err := writeAll(w.writer, output); err != nil {
		return 0, err
	}
	if command != "" {
		if err := writeAll(w.writer, []byte(command)); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

func nativeCursorView(view string) string {
	index := strings.Index(view, nativeCursorPositionMarker)
	action := "hide"
	column, row := 0, 0
	if index < 0 {
		return addNativeCursorCommand(view, nativeCursorCommand(action, column, row))
	}

	before := view[:index]
	lastLine := before[strings.LastIndex(before, "\n")+1:]
	action = "show"
	column = ansi.StringWidth(lastLine) + 1
	row = strings.Count(before, "\n") + 1
	view = strings.Replace(view, nativeCursorPositionMarker, "", 1)
	return addNativeCursorCommand(view, nativeCursorCommand(action, column, row))
}

func addNativeCursorCommand(view, command string) string {
	lines := strings.Split(view, "\n")
	for index := range lines {
		lines[index] = command + lines[index]
	}
	return strings.Join(lines, "\n")
}

func nativeCursorCommand(action string, column, row int) string {
	return fmt.Sprintf("%s%s;%d;%d%s", nativeCursorCommandPrefix, action, column, row, nativeCursorCommandSuffix)
}

func stripNativeCursorCommands(p []byte) ([]byte, string) {
	if !bytes.Contains(p, []byte(nativeCursorCommandPrefix)) {
		return p, ""
	}

	output := make([]byte, 0, len(p))
	command := ""
	for len(p) > 0 {
		start := bytes.Index(p, []byte(nativeCursorCommandPrefix))
		if start < 0 {
			output = append(output, p...)
			break
		}
		output = append(output, p[:start]...)
		commandStart := start + len(nativeCursorCommandPrefix)
		endOffset := bytes.Index(p[commandStart:], []byte(nativeCursorCommandSuffix))
		if endOffset < 0 {
			output = append(output, p[start:]...)
			break
		}
		end := commandStart + endOffset
		if parsed := parseNativeCursorCommand(p[commandStart:end]); parsed != "" {
			command = parsed
		}
		p = p[end+len(nativeCursorCommandSuffix):]
	}
	return output, command
}

func parseNativeCursorCommand(command []byte) string {
	fields := strings.Split(string(command), ";")
	if len(fields) != 3 {
		return ""
	}
	switch fields[0] {
	case "hide":
		return ansi.HideCursor
	case "show":
		column, columnErr := strconv.Atoi(fields[1])
		row, rowErr := strconv.Atoi(fields[2])
		if columnErr != nil || rowErr != nil || column < 1 || row < 1 {
			return ""
		}
		return ansi.CursorPosition(column, row) + ansi.ShowCursor
	default:
		return ""
	}
}

func writeAll(writer io.Writer, p []byte) error {
	for len(p) > 0 {
		written, err := writer.Write(p)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		p = p[written:]
	}
	return nil
}
