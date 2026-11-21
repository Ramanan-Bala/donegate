package claude

import (
	"encoding/json"
	"fmt"
	"io"
)

// SessionStartInput is the stdin JSON payload sent by Claude Code's SessionStart hook.
type SessionStartInput struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	Source    string `json:"source"`
}

type sessionStartHookSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

type sessionStartOutput struct {
	HookSpecificOutput sessionStartHookSpecificOutput `json:"hookSpecificOutput"`
}

// ParseSessionStart decodes Claude Code's SessionStart hook JSON from r.
func ParseSessionStart(r io.Reader) (SessionStartInput, error) {
	var in SessionStartInput
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return SessionStartInput{}, fmt.Errorf("parse session-start input: %w", err)
	}
	return in, nil
}

// WriteSessionStart writes Claude Code's SessionStart hook JSON with additionalContext.
func WriteSessionStart(w io.Writer, additionalContext string) error {
	out := sessionStartOutput{
		HookSpecificOutput: sessionStartHookSpecificOutput{
			HookEventName:     "SessionStart",
			AdditionalContext: additionalContext,
		},
	}
	if err := json.NewEncoder(w).Encode(out); err != nil {
		return fmt.Errorf("encode session-start output: %w", err)
	}
	return nil
}
