// Package agy implements the Antigravity CLI (agy) hook JSON adapter.
package agy

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Ramanan-Bala/donegate/internal/gate"
	"github.com/Ramanan-Bala/donegate/internal/guard"
)

var pathArgKeys = []string{
	"Path",
	"TargetFile",
	"AbsolutePath",
	"FilePath",
	"File",
	"Destination",
	"Source",
}

// CommonInput holds the shared metadata fields present on every agy hook stdin payload.
type CommonInput struct {
	ConversationID string   `json:"conversationId"`
	Cwd            string   `json:"cwd"`
	WorkspacePaths []string `json:"workspacePaths"`
	HookEventName  string   `json:"hookEventName"`
}

func resolveCwd(cwd string, workspacePaths []string) string {
	if cwd != "" {
		return cwd
	}
	if len(workspacePaths) > 0 {
		return workspacePaths[0]
	}
	return ""
}

// SessionStartInput is the stdin JSON payload sent by agy's SessionStart hook.
type SessionStartInput struct {
	ConversationID string   `json:"conversationId"`
	Cwd            string   `json:"cwd"`
	WorkspacePaths []string `json:"workspacePaths"`
	HookEventName  string   `json:"hookEventName"`
}

type systemMessageBody struct {
	SystemMessage string `json:"systemMessage"`
}

type injectStep struct {
	SystemMessage systemMessageBody `json:"systemMessage"`
}

type sessionStartOutput struct {
	InjectSteps []injectStep `json:"injectSteps"`
}

// ToolCall represents the toolCall object in agy's PreToolUse stdin payload.
type ToolCall struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type preToolUseInput struct {
	CommonInput
	ToolCall ToolCall `json:"toolCall"`
}

type preToolUseOutput struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}

// StopInput is the stdin JSON payload sent by agy's Stop hook.
type StopInput struct {
	ConversationID     string   `json:"conversationId"`
	Cwd                string   `json:"cwd"`
	WorkspacePaths     []string `json:"workspacePaths"`
	HookEventName      string   `json:"hookEventName"`
	TerminationReason  string   `json:"terminationReason"`
	ContinuationActive bool     `json:"continuationActive"`
	ExecutionNum       int      `json:"executionNum"`
}

type stopContinueOutput struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

// ParseSessionStart decodes agy's SessionStart hook JSON from r and resolves Cwd.
func ParseSessionStart(r io.Reader) (SessionStartInput, error) {
	var in SessionStartInput
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return SessionStartInput{}, fmt.Errorf("parse session-start input: %w", err)
	}
	in.Cwd = resolveCwd(in.Cwd, in.WorkspacePaths)
	return in, nil
}

// WriteSessionStart writes agy's SessionStart hook JSON injecting context as a systemMessage.
func WriteSessionStart(w io.Writer, context string) error {
	out := sessionStartOutput{
		InjectSteps: []injectStep{
			{
				SystemMessage: systemMessageBody{
					SystemMessage: context,
				},
			},
		},
	}
	if err := encodeJSON(w, out); err != nil {
		return fmt.Errorf("encode session-start output: %w", err)
	}
	return nil
}

// ParsePreToolUse decodes agy's PreToolUse hook JSON into a guard.ToolCall.
func ParsePreToolUse(r io.Reader) (guard.ToolCall, error) {
	var in preToolUseInput
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return guard.ToolCall{}, fmt.Errorf("parse pre-tool-use input: %w", err)
	}

	call := guard.ToolCall{
		Cwd: resolveCwd(in.Cwd, in.WorkspacePaths),
	}

	lower := strings.ToLower(strings.TrimSpace(in.ToolCall.Name))
	switch {
	case isCommandTool(lower):
		call.Tool = "Bash"
		call.Command = stringArg(in.ToolCall.Args, "CommandLine")
		if call.Command == "" {
			call.Command = stringArg(in.ToolCall.Args, "Command")
		}
	case isWriteTool(lower):
		call.Tool = "Write"
		for _, key := range pathArgKeys {
			if p := stringArg(in.ToolCall.Args, key); p != "" {
				call.Paths = append(call.Paths, p)
			}
		}
	default:
		call.Tool = in.ToolCall.Name
	}

	return call, nil
}

// WritePreToolUse writes agy's PreToolUse deny JSON when d.Allow is false,
// and writes {"decision":"ask"} when donegate has no objection (deferring to agy permission rules).
func WritePreToolUse(w io.Writer, d guard.Decision) error {
	if d.Allow {
		return WritePreToolUseAsk(w)
	}
	out := preToolUseOutput{
		Decision: "deny",
		Reason:   d.Reason,
	}
	if err := encodeJSON(w, out); err != nil {
		return fmt.Errorf("encode pre-tool-use output: %w", err)
	}
	return nil
}

// WritePreToolUseAsk writes agy's neutral PreToolUse response ({"decision":"ask"}).
func WritePreToolUseAsk(w io.Writer) error {
	if err := encodeJSON(w, preToolUseOutput{Decision: "ask"}); err != nil {
		return fmt.Errorf("encode pre-tool-use ask output: %w", err)
	}
	return nil
}

// ParseStop decodes agy's Stop hook JSON from r and resolves Cwd.
func ParseStop(r io.Reader) (StopInput, error) {
	var in StopInput
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return StopInput{}, fmt.Errorf("parse stop input: %w", err)
	}
	in.Cwd = resolveCwd(in.Cwd, in.WorkspacePaths)
	return in, nil
}

// WriteStop writes agy's Stop hook JSON response for o.
// When not blocked, it writes {} to w and prints any UserMessage to errW.
func WriteStop(w, errW io.Writer, o *gate.Outcome) error {
	if o == nil {
		return WriteEmpty(w)
	}
	if o.Block {
		out := stopContinueOutput{
			Decision: "continue",
			Reason:   o.Message,
		}
		if err := encodeJSON(w, out); err != nil {
			return fmt.Errorf("encode stop output: %w", err)
		}
		return nil
	}
	if err := WriteEmpty(w); err != nil {
		return err
	}
	if o.UserMessage != "" && errW != nil {
		fmt.Fprintln(errW, o.UserMessage)
	}
	return nil
}

// WriteEmpty writes an empty JSON object line ({}) to w.
func WriteEmpty(w io.Writer) error {
	if err := encodeJSON(w, struct{}{}); err != nil {
		return fmt.Errorf("encode empty output: %w", err)
	}
	return nil
}

func encodeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func isCommandTool(lowerName string) bool {
	return lowerName == "run_command" ||
		strings.Contains(lowerName, "command") ||
		strings.Contains(lowerName, "shell") ||
		strings.Contains(lowerName, "terminal")
}

func isWriteTool(lowerName string) bool {
	return strings.Contains(lowerName, "write") ||
		strings.Contains(lowerName, "edit") ||
		strings.Contains(lowerName, "replace") ||
		strings.Contains(lowerName, "delete") ||
		strings.Contains(lowerName, "move") ||
		strings.Contains(lowerName, "create")
}

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}
