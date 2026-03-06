package agentrun

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type CommandRequest struct {
	WorkDir        string
	Args           []string
	Stdin          []byte
	Env            []string
	StdoutWriter   io.Writer
	StderrWriter   io.Writer
	ProgressWriter io.Writer
}

type CommandResult struct {
	Stdout []byte
	Stderr []byte
}

type CommandRunner interface {
	Run(ctx context.Context, req CommandRequest) (CommandResult, error)
}

type ExecCommandRunner struct{}

func (ExecCommandRunner) Run(ctx context.Context, req CommandRequest) (CommandResult, error) {
	if len(req.Args) == 0 {
		return CommandResult{}, errors.New("missing command")
	}

	cmd := exec.CommandContext(ctx, req.Args[0], req.Args[1:]...)
	cmd.Dir = req.WorkDir
	cmd.Stdin = bytes.NewReader(req.Stdin)
	if len(req.Env) > 0 {
		cmd.Env = req.Env
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = selectStdoutWriter(&stdout, req.StdoutWriter, req.ProgressWriter)
	cmd.Stderr = selectStderrWriter(&stderr, req.StderrWriter, req.ProgressWriter)

	err := cmd.Run()
	result := CommandResult{
		Stdout: stdout.Bytes(),
		Stderr: stderr.Bytes(),
	}
	if err != nil {
		return result, fmt.Errorf("%s: %w: %s", strings.Join(req.Args, " "), err, formatCommandFailure(stdout.String(), stderr.String()))
	}

	return result, nil
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
