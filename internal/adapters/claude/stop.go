package claude

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/Ramanan-Bala/donegate/internal/gate"
)

// StopInput is the stdin JSON payload sent by Claude Code Stop and SubagentStop hooks.
type StopInput struct {
	SessionID      string `json:"session_id"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	StopHookActive bool   `json:"stop_hook_active"`
}

type stopBlockOutput struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

type stopSystemOutput struct {
	SystemMessage string `json:"systemMessage"`
}

// ParseStop decodes Claude Code's Stop/SubagentStop hook JSON from r.
func ParseStop(r io.Reader) (StopInput, error) {
	var in StopInput
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return StopInput{}, fmt.Errorf("parse stop input: %w", err)
	}
	return in, nil
}

// WriteStop writes Claude Code's Stop hook JSON response for o.
func WriteStop(w io.Writer, o *gate.Outcome) error {
	if o == nil {
		return nil
	}
	if o.Block {
		if err := json.NewEncoder(w).Encode(stopBlockOutput{
			Decision: "block",
			Reason:   o.Message,
		}); err != nil {
			return fmt.Errorf("encode stop block output: %w", err)
		}
		return nil
	}
	if o.UserMessage != "" {
		if err := json.NewEncoder(w).Encode(stopSystemOutput{
			SystemMessage: o.UserMessage,
		}); err != nil {
			return fmt.Errorf("encode stop system output: %w", err)
		}
	}
	return nil
}
