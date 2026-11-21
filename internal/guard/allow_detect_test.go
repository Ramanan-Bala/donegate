package guard

import "testing"

func TestIsDonegateAllowCommandDetection(t *testing.T) {
	ctx := Context{RepoRoot: t.TempDir()}
	wantReason := "donegate: only a human can approve exceptions (donegate allow); ask the user to run it in their terminal."

	tests := []struct {
		name      string
		command   string
		wantAllow bool
	}{
		// Bypasses that must be denied
		{name: "sh -c", command: `sh -c "donegate allow T001 x"`, wantAllow: false},
		{name: "bash -lc", command: `bash -lc 'donegate allow all y'`, wantAllow: false},
		{name: "zsh -ic", command: `zsh -ic "donegate allow T001 x"`, wantAllow: false},
		{name: "dash -c", command: `/bin/dash -c "donegate allow T001 x"`, wantAllow: false},
		{name: "ksh -c", command: `ksh -c "donegate allow T001 x"`, wantAllow: false},
		{name: "env with var", command: `env FOO=1 donegate allow T001 x`, wantAllow: false},
		{name: "env with flags", command: `env -i -u HOME FOO=1 donegate allow T001 x`, wantAllow: false},
		{name: "exec wrapper", command: `exec donegate allow T001 x`, wantAllow: false},
		{name: "command wrapper", command: `command donegate allow T001 x`, wantAllow: false},
		{name: "nohup wrapper", command: `nohup donegate allow T001 x`, wantAllow: false},
		{name: "sudo wrapper", command: `sudo donegate allow T001 x`, wantAllow: false},
		{name: "sudo with flags", command: `sudo -u root -E donegate allow T001 x`, wantAllow: false},
		{name: "time wrapper", command: `time donegate allow T001 x`, wantAllow: false},
		{name: "xargs wrapper", command: `xargs donegate allow`, wantAllow: false},
		{name: "xargs with flags", command: `echo T001 x | xargs -n 2 donegate allow`, wantAllow: false},
		{name: "timeout wrapper", command: `timeout 10 donegate allow T001 x`, wantAllow: false},
		{name: "relative ./donegate", command: `./donegate allow T001 x`, wantAllow: false},
		{name: "relative ../bin/donegate", command: `../bin/donegate allow T001 x`, wantAllow: false},
		{name: "command substitution", command: `"$(which donegate)" allow T001 x`, wantAllow: false},
		{name: "backtick substitution", command: "`which donegate` allow T001 x", wantAllow: false},
		{name: "go run cmd/donegate", command: `go run ./cmd/donegate allow T001 x`, wantAllow: false},
		{name: "eval wrapper", command: `eval "donegate allow T001 x"`, wantAllow: false},

		// Legitimate commands that must be allowed
		{name: "echo donegateallow", command: `echo donegateallow`, wantAllow: true},
		{name: "grep allowance", command: `grep -r "allowance" .`, wantAllow: true},
		{name: "go test internal/allow", command: `go test ./internal/allow/...`, wantAllow: true},
		{name: "donegate report", command: `donegate report`, wantAllow: true},
		{name: "cat donegate allow-list-doc.md", command: `cat donegate allow-list-doc.md`, wantAllow: true},
		{name: "cat quoted donegate allow-list-doc.md", command: `cat "donegate allow-list-doc.md"`, wantAllow: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dec := Decide(ToolCall{
				Tool:    "Bash",
				Command: tc.command,
				Cwd:     ctx.RepoRoot,
			}, ctx)
			if dec.Allow != tc.wantAllow {
				t.Fatalf("Decide(%q).Allow = %v, want %v (dec=%+v)", tc.command, dec.Allow, tc.wantAllow, dec)
			}
			if !tc.wantAllow && (dec.RuleID != "H001" || dec.Reason != wantReason) {
				t.Fatalf("Decide(%q) = %+v, want H001 with reason %q", tc.command, dec, wantReason)
			}
		})
	}
}
