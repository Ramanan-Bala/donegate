package claude

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/Ramanan-Bala/donegate/internal/guard"
)

type preToolUseInput struct {
	SessionID     string          `json:"session_id"`
	Cwd           string          `json:"cwd"`
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
}

type fileToolInput struct {
	FilePath string `json:"file_path"`
}

type notebookToolInput struct {
	NotebookPath string `json:"notebook_path"`
}

type bashToolInput struct {
	Command string `json:"command"`
}

type hookSpecificOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}

type preToolUseOutput struct {
	HookSpecificOutput hookSpecificOutput `json:"hookSpecificOutput"`
}

// ParsePreToolUse decodes Claude Code's PreToolUse hook JSON into a guard.ToolCall.
func ParsePreToolUse(r io.Reader) (guard.ToolCall, error) {
	var in preToolUseInput
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return guard.ToolCall{}, fmt.Errorf("parse pre-tool-use input: %w", err)
	}

	call := guard.ToolCall{
		Tool: in.ToolName,
		Cwd:  in.Cwd,
	}

	switch in.ToolName {
	case "Write", "Edit", "MultiEdit":
		if len(in.ToolInput) > 0 && string(in.ToolInput) != "null" {
			var ti fileToolInput
			if err := json.Unmarshal(in.ToolInput, &ti); err != nil {
				return guard.ToolCall{}, fmt.Errorf("parse %s tool_input: %w", in.ToolName, err)
			}
			if ti.FilePath != "" {
				call.Paths = []string{ti.FilePath}
			}
		}
	case "NotebookEdit":
		if len(in.ToolInput) > 0 && string(in.ToolInput) != "null" {
			var ti notebookToolInput
			if err := json.Unmarshal(in.ToolInput, &ti); err != nil {
				return guard.ToolCall{}, fmt.Errorf("parse NotebookEdit tool_input: %w", err)
			}
			if ti.NotebookPath != "" {
				call.Paths = []string{ti.NotebookPath}
			}
		}
	case "Bash":
		if len(in.ToolInput) > 0 && string(in.ToolInput) != "null" {
			var ti bashToolInput
			if err := json.Unmarshal(in.ToolInput, &ti); err != nil {
				return guard.ToolCall{}, fmt.Errorf("parse Bash tool_input: %w", err)
			}
			call.Command = ti.Command
		}
	}

	return call, nil
}

// WritePreToolUse writes Claude Code's PreToolUse deny JSON when d.Allow is false,
// and writes nothing when d.Allow is true.
func WritePreToolUse(w io.Writer, d guard.Decision) error {
	if d.Allow {
		return nil
	}
	out := preToolUseOutput{
		HookSpecificOutput: hookSpecificOutput{
			HookEventName:            "PreToolUse",
			PermissionDecision:       "deny",
			PermissionDecisionReason: d.Reason,
		},
	}
	if err := json.NewEncoder(w).Encode(out); err != nil {
		return fmt.Errorf("encode pre-tool-use output: %w", err)
	}
	return nil
}
