package jsonhook

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Ramanan-Bala/donegate/internal/gate"
	"github.com/Ramanan-Bala/donegate/internal/guard"
)

// GuardInput is the stdin JSON payload for `donegate guard --format json`.
type GuardInput struct {
	Tool    string   `json:"tool"`
	Paths   []string `json:"paths"`
	Command string   `json:"command"`
	Cwd     string   `json:"cwd"`
	Session string   `json:"session,omitempty"`
}

// GuardOutput is the stdout JSON payload for `donegate guard --format json`.
type GuardOutput struct {
	Allow  bool   `json:"allow"`
	Rule   string `json:"rule,omitempty"`
	Path   string `json:"path,omitempty"`
	Reason string `json:"reason,omitempty"`
	Error  string `json:"error,omitempty"`
}

// GateInput is the stdin JSON payload for `donegate gate --format json`.
type GateInput struct {
	Cwd     string `json:"cwd"`
	Session string `json:"session,omitempty"`
}

// GateOutput is the stdout JSON payload for `donegate gate --format json`.
type GateOutput struct {
	Block       bool   `json:"block"`
	Message     string `json:"message,omitempty"`
	UserMessage string `json:"user_message,omitempty"`
	Verdict     string `json:"verdict,omitempty"`
	Attempt     *int   `json:"attempt,omitempty"`
	MaxBlocks   int    `json:"max_blocks,omitempty"`
	Evidence    string `json:"evidence,omitempty"`
	Error       string `json:"error,omitempty"`
}

// SessionStartInput is the stdin JSON payload for `donegate session start --format json`.
type SessionStartInput struct {
	Cwd     string `json:"cwd"`
	Session string `json:"session"`
	Source  string `json:"source"`
}

// SessionStartOutput is the stdout JSON payload for `donegate session start --format json`.
type SessionStartOutput struct {
	Session string `json:"session,omitempty"`
	Reused  *bool  `json:"reused,omitempty"`
	Context string `json:"context,omitempty"`
	Skipped bool   `json:"skipped,omitempty"`
	Error   string `json:"error,omitempty"`
}

// ParseGuard decodes a harness-neutral guard hook JSON object into a guard.ToolCall.
func ParseGuard(r io.Reader) (guard.ToolCall, error) {
	call, _, err := ParseGuardWithSession(r)
	return call, err
}

// ParseGuardWithSession decodes a harness-neutral guard hook JSON object into a guard.ToolCall and session ID.
func ParseGuardWithSession(r io.Reader) (guard.ToolCall, string, error) {
	var in GuardInput
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return guard.ToolCall{}, "", fmt.Errorf("parse guard input: %w", err)
	}

	call := guard.ToolCall{
		Cwd: in.Cwd,
	}

	switch strings.ToLower(strings.TrimSpace(in.Tool)) {
	case "bash", "shell":
		call.Tool = "Bash"
		call.Command = in.Command
		if len(in.Paths) > 0 {
			call.Paths = in.Paths
		}
	default:
		if len(in.Paths) > 0 {
			call.Tool = "Write"
			call.Paths = in.Paths
		} else {
			call.Tool = in.Tool
		}
	}

	return call, in.Session, nil
}

// WriteGuard writes a guard decision as a single JSON line.
func WriteGuard(w io.Writer, d guard.Decision) error {
	out := GuardOutput{Allow: d.Allow}
	if !d.Allow {
		out.Rule = d.RuleID
		out.Path = d.Path
		out.Reason = d.Reason
	}
	if err := json.NewEncoder(w).Encode(out); err != nil {
		return fmt.Errorf("encode guard output: %w", err)
	}
	return nil
}

// WriteGuardError writes a fail-open guard JSON response carrying err.
func WriteGuardError(w io.Writer, err error) error {
	out := GuardOutput{
		Allow: true,
		Error: errString(err),
	}
	if encErr := json.NewEncoder(w).Encode(out); encErr != nil {
		return fmt.Errorf("encode guard error output: %w", encErr)
	}
	return nil
}

// ParseGate decodes a harness-neutral gate hook JSON object from r.
func ParseGate(r io.Reader) (GateInput, error) {
	var in GateInput
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return GateInput{}, fmt.Errorf("parse gate input: %w", err)
	}
	return in, nil
}

// WriteGate writes a gate outcome as a single JSON line.
func WriteGate(w io.Writer, o *gate.Outcome) error {
	if o == nil || (!o.Block && o.MaxBlocks == 0 && o.EvidencePath == "") {
		if err := json.NewEncoder(w).Encode(GateOutput{Block: false}); err != nil {
			return fmt.Errorf("encode gate output: %w", err)
		}
		return nil
	}

	attempt := o.Attempt
	out := GateOutput{
		Block:       o.Block,
		Message:     o.Message,
		UserMessage: o.UserMessage,
		Verdict:     o.Verdict,
		Attempt:     &attempt,
		MaxBlocks:   o.MaxBlocks,
		Evidence:    o.EvidencePath,
	}
	if err := json.NewEncoder(w).Encode(out); err != nil {
		return fmt.Errorf("encode gate output: %w", err)
	}
	return nil
}

// WriteGateError writes a fail-open gate JSON response carrying err.
func WriteGateError(w io.Writer, err error) error {
	out := GateOutput{
		Block: false,
		Error: errString(err),
	}
	if encErr := json.NewEncoder(w).Encode(out); encErr != nil {
		return fmt.Errorf("encode gate error output: %w", encErr)
	}
	return nil
}

// ParseSessionStart decodes a harness-neutral session-start hook JSON object from r.
func ParseSessionStart(r io.Reader) (SessionStartInput, error) {
	var in SessionStartInput
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return SessionStartInput{}, fmt.Errorf("parse session-start input: %w", err)
	}
	return in, nil
}

// WriteSessionStart writes a started or reused session-start JSON response.
func WriteSessionStart(w io.Writer, sessionID string, reused bool, context string) error {
	r := reused
	out := SessionStartOutput{
		Session: sessionID,
		Reused:  &r,
		Context: context,
	}
	if err := json.NewEncoder(w).Encode(out); err != nil {
		return fmt.Errorf("encode session-start output: %w", err)
	}
	return nil
}

// WriteSessionStartSkipped writes a skipped session-start JSON response (e.g. outside a git repo).
func WriteSessionStartSkipped(w io.Writer) error {
	if err := json.NewEncoder(w).Encode(SessionStartOutput{Skipped: true}); err != nil {
		return fmt.Errorf("encode session-start skipped output: %w", err)
	}
	return nil
}

// WriteSessionStartError writes a fail-open session-start JSON response carrying err.
func WriteSessionStartError(w io.Writer, err error) error {
	out := SessionStartOutput{
		Error: errString(err),
	}
	if encErr := json.NewEncoder(w).Encode(out); encErr != nil {
		return fmt.Errorf("encode session-start error output: %w", encErr)
	}
	return nil
}

func errString(err error) string {
	if err == nil {
		return "unknown error"
	}
	return err.Error()
}
