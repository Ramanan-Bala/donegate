package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/Ramanan-Bala/donegate/internal/allow"
	"github.com/Ramanan-Bala/donegate/internal/session"
)

func TestAllowCmd(t *testing.T) {
	withTTY := func(val bool) func() {
		prev := isAllowTTY
		isAllowTTY = func() bool { return val }
		return func() { isAllowTTY = prev }
	}

	t.Run("non-interactive stdin exits 2 with human-only message", func(t *testing.T) {
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)
		if _, err := session.Start(repo, "manual"); err != nil {
			t.Fatal(err)
		}
		defer withTTY(false)()

		oldStderr := os.Stderr
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Stderr = w
		code := run([]string{"allow", "T001", "src/cart.test.ts", "--repo", repo})
		_ = w.Close()
		os.Stderr = oldStderr

		var errBuf bytes.Buffer
		_, _ = errBuf.ReadFrom(r)
		_ = r.Close()

		if code != 2 {
			t.Errorf("exit code = %d, want 2", code)
		}
		wantMsg := "donegate allow must be run by a human in a terminal; agents cannot approve their own exceptions"
		if !strings.Contains(errBuf.String(), wantMsg) {
			t.Errorf("stderr = %q, want %q", errBuf.String(), wantMsg)
		}
	})

	t.Run("requires exact yes confirmation", func(t *testing.T) {
		t.Setenv("DONEGATE_HOME", t.TempDir())
		repo := initTestRepo(t)
		sess, err := session.Start(repo, "manual")
		if err != nil {
			t.Fatal(err)
		}
		defer withTTY(true)()

		for _, reply := range []string{"y\n", "no\n", "YES\n", "\n"} {
			stdout, _, err := execCmd(t, reply, "allow", "T001", "src/cart.test.ts", "--repo", repo)
			if err == nil {
				t.Fatalf("reply %q unexpectedly succeeded (stdout=%q)", reply, stdout)
			}
			list, listErr := allow.List(sess)
			if listErr != nil {
				t.Fatal(listErr)
			}
			if len(list) != 0 {
				t.Fatalf("reply %q wrote allowance: %+v", reply, list)
			}
		}
	})

	t.Run("interactive yes adds allowance, supports --session, --list, and --revoke", func(t *testing.T) {
		t.Setenv("DONEGATE_HOME", t.TempDir())
		t.Setenv("USER", "alice")
		repo := initTestRepo(t)

		sessA, err := session.StartWith(repo, session.StartOptions{Harness: "claude-code", HarnessSession: "a"})
		if err != nil {
			t.Fatal(err)
		}
		// Start another session so `current` points away from sessA.
		if _, err := session.StartWith(repo, session.StartOptions{Harness: "claude-code", HarnessSession: "b"}); err != nil {
			t.Fatal(err)
		}
		defer withTTY(true)()

		// Allow on sessA explicitly with --session.
		stdout, stderr, err := execCmd(t, "yes\n", "allow", "T001", "src/cart.test.ts", "--repo", repo, "--session", sessA.ID)
		if err != nil {
			t.Fatalf("allow failed: %v (stderr=%q)", err, stderr)
		}
		if !strings.Contains(stdout, sessA.ID) || !strings.Contains(stdout, "src/cart.test.ts") {
			t.Errorf("unexpected stdout: %q", stdout)
		}

		// List on sessA shows the allowance.
		listOut, stderr, err := execCmd(t, "", "allow", "--list", "--repo", repo, "--session", sessA.ID)
		if err != nil {
			t.Fatalf("allow --list failed: %v (stderr=%q)", err, stderr)
		}
		if !strings.Contains(listOut, "T001") || !strings.Contains(listOut, "src/cart.test.ts") || !strings.Contains(listOut, "alice") {
			t.Errorf("unexpected list output: %q", listOut)
		}

		// Revoke on sessA removes it and prints session ID.
		revOut, stderr, err := execCmd(t, "", "allow", "--revoke", "src/cart.test.ts", "--repo", repo, "--session", sessA.ID)
		if err != nil {
			t.Fatalf("allow --revoke failed: %v (stderr=%q)", err, stderr)
		}
		if !strings.Contains(revOut, sessA.ID) {
			t.Errorf("expected session ID in revoke output, got %q", revOut)
		}
		remaining, err := allow.List(sessA)
		if err != nil {
			t.Fatal(err)
		}
		if len(remaining) != 0 {
			t.Errorf("expected empty allowances after revoke, got %+v", remaining)
		}
	})
}
