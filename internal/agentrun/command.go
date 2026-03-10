package agentrun

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	sharedcommand "quick-ai-toolhub/internal/command"
)

type CommandRequest struct {
	WorkDir        string
	Args           []string
	Stdin          []byte
	Env            []string
	Metadata       CommandMetadata
	StdoutWriter   io.Writer
	StderrWriter   io.Writer
	ProgressWriter io.Writer
}

type CommandMetadata struct {
	Model       string
	Sandbox     string
	EnvKeys     []string
	EnvSnapshot map[string]string
}

type CommandResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	TimedOut bool
	Err      error
}

type CommandRunner interface {
	Run(ctx context.Context, req CommandRequest) (CommandResult, error)
}

type ExecCommandRunner struct{}

func (ExecCommandRunner) Run(ctx context.Context, req CommandRequest) (CommandResult, error) {
	if len(req.Args) == 0 {
		return CommandResult{}, errors.New("missing command")
	}

	writeCommandMetadata(req.StdoutWriter, req.ProgressWriter, req.Metadata)

	result := sharedcommand.Executor{}.Run(ctx, sharedcommand.Request{
		WorkDir:      req.WorkDir,
		Args:         req.Args,
		Stdin:        req.Stdin,
		Env:          req.Env,
		StdoutWriter: selectStdoutWriter(&bytes.Buffer{}, req.StdoutWriter, req.ProgressWriter),
		StderrWriter: selectStderrWriter(&bytes.Buffer{}, req.StderrWriter, req.ProgressWriter),
	})
	commandResult := CommandResult{
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: result.ExitCode,
		TimedOut: result.TimedOut,
		Err:      result.Err,
	}
	if result.Err != nil {
		return commandResult, fmt.Errorf("%s: %w: %s", strings.Join(req.Args, " "), result.Err, formatCommandFailure(string(result.Stdout), string(result.Stderr)))
	}

	return commandResult, nil
}

func writeCommandMetadata(stream, progress io.Writer, meta CommandMetadata) {
	lines := formatCommandMetadata(meta)
	if len(lines) == 0 {
		return
	}
	for _, line := range lines {
		if progress != nil {
			fmt.Fprintf(progress, "[progress] %s\n", line)
		}
		if stream != nil {
			fmt.Fprintf(stream, "[meta] %s\n", line)
		}
	}
}

func formatCommandMetadata(meta CommandMetadata) []string {
	var lines []string
	if strings.TrimSpace(meta.Model) != "" {
		lines = append(lines, fmt.Sprintf("model: %s", meta.Model))
	}
	if strings.TrimSpace(meta.Sandbox) != "" {
		lines = append(lines, fmt.Sprintf("sandbox: %s", meta.Sandbox))
	}
	if len(meta.EnvKeys) > 0 {
		for _, key := range meta.EnvKeys {
			value := strings.TrimSpace(meta.EnvSnapshot[key])
			if value == "" {
				lines = append(lines, fmt.Sprintf("env %s: <empty>", key))
				continue
			}
			lines = append(lines, fmt.Sprintf("env %s: %s", key, value))
		}
	}
	return lines
}

func selectStdoutWriter(buffer *bytes.Buffer, stream, progress io.Writer) io.Writer {
	if stream == nil {
		if progress == nil {
			return buffer
		}
		return io.MultiWriter(buffer, newProgressEventWriter(progress))
	}
	return io.MultiWriter(buffer, stream)
}

func selectStderrWriter(buffer *bytes.Buffer, stream, progress io.Writer) io.Writer {
	if stream == nil {
		if progress == nil {
			return buffer
		}
		return io.MultiWriter(buffer, newProgressLineWriter(progress, "[stderr] "))
	}
	return io.MultiWriter(buffer, stream)
}

func formatCommandFailure(stdout, stderr string) string {
	var parts []string

	stdout = strings.TrimSpace(stdout)
	stderr = strings.TrimSpace(stderr)

	if stderr != "" {
		parts = append(parts, "stderr: "+stderr)
	}
	if stdout != "" {
		parts = append(parts, "stdout: "+stdout)
	}
	if len(parts) == 0 {
		return "no stdout or stderr output"
	}

	return strings.Join(parts, " | ")
}

type progressEventWriter struct {
	out io.Writer
	buf bytes.Buffer
}

func newProgressEventWriter(out io.Writer) io.Writer {
	return &progressEventWriter{out: out}
}

func (w *progressEventWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	reader := bufio.NewReader(&w.buf)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			w.buf.Reset()
			w.buf.WriteString(line)
			return len(p), nil
		}
		w.handleLine(strings.TrimSpace(line))
	}
}

func (w *progressEventWriter) handleLine(line string) {
	if line == "" {
		return
	}

	var event map[string]any
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return
	}

	eventType, _ := event["type"].(string)
	switch eventType {
	case "thread.started":
		fmt.Fprintln(w.out, "[progress] agent started")
	case "item.started", "item.updated", "item.completed":
		item, _ := event["item"].(map[string]any)
		itemType, _ := item["type"].(string)
		if itemType == "todo_list" {
			if summary := summarizeTodoList(item["items"]); summary != "" {
				fmt.Fprintf(w.out, "[progress] %s\n", summary)
			}
		}
	case "turn.completed":
		fmt.Fprintln(w.out, "[progress] agent finished")
	case "system":
		if subtype, _ := event["subtype"].(string); subtype == "init" {
			fmt.Fprintln(w.out, "[progress] agent started")
		}
	case "assistant":
		if summary := summarizeClaudeAssistant(event); summary != "" {
			fmt.Fprintf(w.out, "[progress] %s\n", summary)
		}
	case "result":
		fmt.Fprintln(w.out, "[progress] agent finished")
	}
}

func summarizeTodoList(value any) string {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return ""
	}

	completed := 0
	var current string
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		text, _ := item["text"].(string)
		done, _ := item["completed"].(bool)
		if done {
			completed++
			continue
		}
		if current == "" {
			current = text
		}
	}
	if current == "" {
		return fmt.Sprintf("todo %d/%d completed", completed, len(items))
	}
	return fmt.Sprintf("todo %d/%d, current: %s", completed, len(items), current)
}

func summarizeClaudeAssistant(event map[string]any) string {
	message, _ := event["message"].(map[string]any)
	content, _ := message["content"].([]any)
	if len(content) == 0 {
		return ""
	}

	for _, raw := range content {
		part, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch partType, _ := part["type"].(string); partType {
		case "tool_use":
			name, _ := part["name"].(string)
			if strings.TrimSpace(name) != "" {
				return fmt.Sprintf("using tool: %s", name)
			}
		case "text":
			text, _ := part["text"].(string)
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			text = strings.Join(strings.Fields(text), " ")
			if len(text) > 80 {
				text = text[:77] + "..."
			}
			return fmt.Sprintf("assistant: %s", text)
		}
	}
	return ""
}

type progressLineWriter struct {
	out    io.Writer
	prefix string
	buf    bytes.Buffer
}

func newProgressLineWriter(out io.Writer, prefix string) io.Writer {
	return &progressLineWriter{out: out, prefix: prefix}
}

func (w *progressLineWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	reader := bufio.NewReader(&w.buf)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			w.buf.Reset()
			w.buf.WriteString(line)
			return len(p), nil
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fmt.Fprintf(w.out, "%s%s\n", w.prefix, line)
	}
}
