package gate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ramanan-Bala/donegate/internal/evidence"
	"github.com/Ramanan-Bala/donegate/internal/session"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeRepoFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", full, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

func initRepo(t *testing.T, extraFiles map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "-c", "init.defaultBranch=main", "init")
	writeRepoFile(t, dir, "src/cart.ts", "export const total = () => 1;\n")
	writeRepoFile(t, dir, "src/cart.test.ts", "test('total', () => expect(total()).toBe(1));\n")
	for rel, content := range extraFiles {
		writeRepoFile(t, dir, rel, content)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "initial")
	return dir
}

func fixedNow() func() time.Time {
	t0 := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return t0 }
}

func TestRunNoSession(t *testing.T) {
	t.Setenv("DONEGATE_HOME", t.TempDir())
	dir := initRepo(t, nil)

	out, err := Run(Options{Cwd: dir, Now: fixedNow()})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if out.Block {
		t.Fatalf("expected Block=false, got true")
	}
	if !strings.Contains(out.UserMessage, "no session for this repo") {
		t.Errorf("unexpected UserMessage: %q", out.UserMessage)
	}
}

func TestRunNoChangesDoesNotWriteStateOrEvidence(t *testing.T) {
	t.Setenv("DONEGATE_HOME", t.TempDir())
	dir := initRepo(t, nil)

	sess, err := session.Start(dir, "claude-code")
	if err != nil {
		t.Fatalf("session.Start failed: %v", err)
	}

	out, err := Run(Options{Cwd: dir, Now: fixedNow()})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if out.Block || out.Verdict != "PASS" {
		t.Fatalf("got Block=%v Verdict=%q, want Block=false Verdict=PASS", out.Block, out.Verdict)
	}
	if out.EvidencePath != "" {
		t.Errorf("expected empty EvidencePath, got %q", out.EvidencePath)
	}
	if _, err := os.Stat(filepath.Join(sess.Dir(), "gate.json")); !os.IsNotExist(err) {
		t.Errorf("gate.json should not exist when no changes, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(sess.Dir(), "evidence", "evidence.json")); !os.IsNotExist(err) {
		t.Errorf("evidence.json should not exist when no changes, stat err=%v", err)
	}
}

func TestRunHonestSourceEditPassesAndWritesEvidence(t *testing.T) {
	t.Setenv("DONEGATE_HOME", t.TempDir())
	dir := initRepo(t, nil)

	sess, err := session.Start(dir, "claude-code")
	if err != nil {
		t.Fatalf("session.Start failed: %v", err)
	}

	writeRepoFile(t, dir, "src/cart.ts", "export const total = () => 2;\n")

	out, err := Run(Options{Cwd: dir, Now: fixedNow()})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if out.Block || out.Verdict != "PASS" {
		t.Fatalf("got Block=%v Verdict=%q Message=%q", out.Block, out.Verdict, out.Message)
	}
	if !strings.Contains(out.UserMessage, "donegate: verified (0 warnings).") {
		t.Errorf("unexpected UserMessage: %q", out.UserMessage)
	}
	if out.EvidencePath == "" {
		t.Fatal("expected EvidencePath to be set")
	}

	ev, err := evidence.Read(out.EvidencePath)
	if err != nil {
		t.Fatalf("evidence.Read failed: %v", err)
	}
	if err := ev.Verify(); err != nil {
		t.Fatalf("evidence.Verify failed: %v", err)
	}
	if ev.Verdict != "PASS" || ev.Session != sess.ID {
		t.Errorf("unexpected evidence: verdict=%q session=%q", ev.Verdict, ev.Session)
	}
}

func TestRunEditExistingTestBlocksWithT001AndH001(t *testing.T) {
	t.Setenv("DONEGATE_HOME", t.TempDir())
	dir := initRepo(t, nil)

	if _, err := session.Start(dir, "claude-code"); err != nil {
		t.Fatalf("session.Start failed: %v", err)
	}

	writeRepoFile(t, dir, "src/cart.test.ts", "test('total', () => expect(total()).toBe(99));\n")

	out, err := Run(Options{Cwd: dir, Now: fixedNow()})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !out.Block || out.Verdict != "FAIL" {
		t.Fatalf("got Block=%v Verdict=%q, want Block=true Verdict=FAIL", out.Block, out.Verdict)
	}
	if out.Attempt != 1 || out.MaxBlocks != 5 {
		t.Errorf("Attempt/MaxBlocks = %d/%d, want 1/5", out.Attempt, out.MaxBlocks)
	}
	// H001 fires too, but the message drops it because T001 already reports the same path.
	if !strings.Contains(out.Message, "donegate: not done (1 failure, attempt 1/5)") {
		t.Errorf("unexpected header in Message:\n%s", out.Message)
	}
	if strings.Contains(out.Message, "- H001 src/cart.test.ts") {
		t.Errorf("duplicate H001 line should be suppressed in Message:\n%s", out.Message)
	}
	if !strings.Contains(out.Message, "- T001 src/cart.test.ts was modified.") {
		t.Errorf("missing T001 line in Message:\n%s", out.Message)
	}
	if !strings.HasSuffix(out.Message, "Fix these and stop again; donegate re-checks every stop.") {
		t.Errorf("unexpected footer in Message:\n%s", out.Message)
	}
}

func TestRunMaxBlocksExceededAndPreservesBlocksCountOnPass(t *testing.T) {
	t.Setenv("DONEGATE_HOME", t.TempDir())
	dir := initRepo(t, map[string]string{
		".donegate.yml": "version: 1\ngate:\n  max_blocks: 2\n",
	})

	if _, err := session.Start(dir, "claude-code"); err != nil {
		t.Fatalf("session.Start failed: %v", err)
	}

	writeRepoFile(t, dir, "src/cart.test.ts", "test('total', () => expect(total()).toBe(99));\n")

	out1, err := Run(Options{Cwd: dir, Now: fixedNow()})
	if err != nil {
		t.Fatalf("run 1 failed: %v", err)
	}
	if !out1.Block || out1.Attempt != 1 || out1.MaxBlocks != 2 {
		t.Fatalf("run 1: %+v", out1)
	}

	out2, err := Run(Options{Cwd: dir, Now: fixedNow()})
	if err != nil {
		t.Fatalf("run 2 failed: %v", err)
	}
	if !out2.Block || out2.Attempt != 2 || out2.MaxBlocks != 2 {
		t.Fatalf("run 2: %+v", out2)
	}

	out3, err := Run(Options{Cwd: dir, Now: fixedNow()})
	if err != nil {
		t.Fatalf("run 3 failed: %v", err)
	}
	if out3.Block || out3.Verdict != "FAIL" || out3.Attempt != 3 {
		t.Fatalf("run 3: want Block=false Verdict=FAIL Attempt=3, got %+v", out3)
	}
	if !strings.Contains(out3.UserMessage, "gave up after 2 blocks; the work is NOT verified.") {
		t.Errorf("unexpected run 3 UserMessage: %q", out3.UserMessage)
	}

	ev, err := evidence.Read(out3.EvidencePath)
	if err != nil {
		t.Fatalf("evidence.Read failed: %v", err)
	}
	if ev.Verdict != "FAIL" || ev.Blocks != 3 {
		t.Errorf("evidence = verdict:%q blocks:%d, want FAIL/3", ev.Verdict, ev.Blocks)
	}

	// Now revert the test and make an honest source edit; blocks count must be kept in evidence.
	writeRepoFile(t, dir, "src/cart.test.ts", "test('total', () => expect(total()).toBe(1));\n")
	writeRepoFile(t, dir, "src/cart.ts", "export const total = () => 3;\n")

	out4, err := Run(Options{Cwd: dir, Now: fixedNow()})
	if err != nil {
		t.Fatalf("run 4 failed: %v", err)
	}
	if out4.Block || out4.Verdict != "PASS" {
		t.Fatalf("run 4: %+v", out4)
	}
	ev4, err := evidence.Read(out4.EvidencePath)
	if err != nil {
		t.Fatalf("evidence.Read run 4 failed: %v", err)
	}
	if ev4.Verdict != "PASS" || ev4.Blocks != 3 {
		t.Errorf("run 4 evidence = verdict:%q blocks:%d, want PASS/3", ev4.Verdict, ev4.Blocks)
	}
}

func TestRunRepoPolicyTamperMidSessionStillUsesSnapshot(t *testing.T) {
	t.Setenv("DONEGATE_HOME", t.TempDir())
	dir := initRepo(t, map[string]string{
		".donegate.yml": "version: 1\nrules:\n  T001: block\n",
	})

	if _, err := session.Start(dir, "claude-code"); err != nil {
		t.Fatalf("session.Start failed: %v", err)
	}

	// Agent weakens repo .donegate.yml and modifies a test file.
	writeRepoFile(t, dir, ".donegate.yml", "version: 1\nrules:\n  T001: off\n  H001: off\n")
	writeRepoFile(t, dir, "src/cart.test.ts", "test('total', () => expect(total()).toBe(99));\n")

	out, err := Run(Options{Cwd: dir, Now: fixedNow()})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !out.Block || out.Verdict != "FAIL" {
		t.Fatalf("expected block using snapshotted policy, got %+v", out)
	}
	if !strings.Contains(out.Message, "H001 .donegate.yml changed since the session started") {
		t.Errorf("expected H001 for .donegate.yml, got:\n%s", out.Message)
	}
	if !strings.Contains(out.Message, "T001 src/cart.test.ts was modified") {
		t.Errorf("expected T001 to still run from snapshot policy, got:\n%s", out.Message)
	}
}

func TestRunSnapshotPolicyFileTamperTriggersH001(t *testing.T) {
	t.Setenv("DONEGATE_HOME", t.TempDir())
	dir := initRepo(t, nil)

	sess, err := session.Start(dir, "claude-code")
	if err != nil {
		t.Fatalf("session.Start failed: %v", err)
	}

	snapshotPolicy := filepath.Join(sess.Dir(), "policy.yml")
	if err := os.WriteFile(snapshotPolicy, []byte("version: 1\ngate:\n  tier: fast\n"), 0o600); err != nil {
		t.Fatalf("tamper snapshot policy: %v", err)
	}

	out, err := Run(Options{Cwd: dir, Now: fixedNow()})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !out.Block || out.Verdict != "FAIL" {
		t.Fatalf("expected Block=true Verdict=FAIL, got %+v", out)
	}
	if !strings.Contains(out.Message, "H001 "+snapshotPolicy+" changed since the session started") {
		t.Errorf("expected H001 for snapshot policy, got:\n%s", out.Message)
	}
}

func TestRunIncludesWarningsInBlockMessageAndPassSummary(t *testing.T) {
	t.Setenv("DONEGATE_HOME", t.TempDir())
	dir := initRepo(t, map[string]string{
		"src/cart.test.ts": "test('total', () => expect(total()).toBe('order-123'));\n",
	})

	if _, err := session.Start(dir, "claude-code"); err != nil {
		t.Fatalf("session.Start failed: %v", err)
	}

	// Trigger S001 warn alongside a T001/H001 block.
	writeRepoFile(t, dir, "src/cart.ts", "export const total = () => 'order-123';\n")
	writeRepoFile(t, dir, "src/cart.test.ts", "test('total', () => expect(total()).toBe('order-999'));\n")

	outFail, err := Run(Options{Cwd: dir, Now: fixedNow()})
	if err != nil {
		t.Fatalf("Run fail case failed: %v", err)
	}
	if !outFail.Block {
		t.Fatalf("expected Block=true, got %+v", outFail)
	}
	if !strings.Contains(outFail.Message, "\nwarn S001 ") {
		t.Errorf("expected warn S001 line in block message, got:\n%s", outFail.Message)
	}

	// Revert test file so only S001 warning remains.
	writeRepoFile(t, dir, "src/cart.test.ts", "test('total', () => expect(total()).toBe('order-123'));\n")

	outPass, err := Run(Options{Cwd: dir, Now: fixedNow()})
	if err != nil {
		t.Fatalf("Run pass case failed: %v", err)
	}
	if outPass.Block || outPass.Verdict != "PASS" {
		t.Fatalf("expected PASS, got %+v", outPass)
	}
	if !strings.Contains(outPass.UserMessage, "donegate: verified (1 warning).") {
		t.Errorf("expected 1 warning in UserMessage, got %q", outPass.UserMessage)
	}
}

func TestRunStandardTierFailingProjectBlocksWithR001AndHint(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("shell command tests use POSIX /bin/sh")
	}
	t.Setenv("DONEGATE_HOME", t.TempDir())

	testCmd := "sh -c 'if grep -q FIXED src/cart.ts; then exit 0; else echo FAIL_CART; exit 1; fi'"
	dir := initRepo(t, map[string]string{
		".donegate.yml": "version: 1\ngate:\n  tier: standard\nprojects:\n  - name: web\n    root: .\n    test: \"" + testCmd + "\"\n",
	})

	if _, err := session.Start(dir, "claude-code"); err != nil {
		t.Fatalf("session.Start failed: %v", err)
	}

	writeRepoFile(t, dir, "src/cart.ts", "export const total = () => 2;\n")

	out, err := Run(Options{Cwd: dir, Now: fixedNow()})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !out.Block || out.Verdict != "FAIL" {
		t.Fatalf("got Block=%v Verdict=%q, want Block=true Verdict=FAIL", out.Block, out.Verdict)
	}
	if !strings.Contains(out.Message, "- R001 ") {
		t.Errorf("expected R001 finding in Message:\n%s", out.Message)
	}
	wantHint := "Run `" + testCmd + "` in . to reproduce."
	if !strings.Contains(out.Message, wantHint) {
		t.Errorf("expected reproduce hint %q in Message:\n%s", wantHint, out.Message)
	}
}

func TestRunFastTierNeverRunsL2(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("shell command tests use POSIX /bin/sh")
	}
	t.Setenv("DONEGATE_HOME", t.TempDir())

	marker := filepath.Join(t.TempDir(), "l2-ran")
	dir := initRepo(t, map[string]string{
		".donegate.yml": "version: 1\ngate:\n  tier: fast\nprojects:\n  - name: web\n    root: .\n    test: \"sh -c 'echo ran > " + marker + "; exit 1'\"\n",
	})

	if _, err := session.Start(dir, "claude-code"); err != nil {
		t.Fatalf("session.Start failed: %v", err)
	}

	writeRepoFile(t, dir, "src/cart.ts", "export const total = () => 2;\n")

	out, err := Run(Options{Cwd: dir, Now: fixedNow()})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if out.Block || out.Verdict != "PASS" {
		t.Fatalf("got Block=%v Verdict=%q Message=%q, want PASS", out.Block, out.Verdict, out.Message)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("fast tier should never run L2, but marker stat err=%v", err)
	}
}

func TestRunH001DuplicateSuppressedWhenT001CoversSamePath(t *testing.T) {
	t.Setenv("DONEGATE_HOME", t.TempDir())
	dir := initRepo(t, nil)

	if _, err := session.Start(dir, "claude-code"); err != nil {
		t.Fatalf("session.Start failed: %v", err)
	}

	writeRepoFile(t, dir, "src/cart.test.ts", "test('total', () => expect(total()).toBe(99));\n")

	out, err := Run(Options{Cwd: dir, Now: fixedNow()})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !out.Block || out.Verdict != "FAIL" {
		t.Fatalf("got Block=%v Verdict=%q, want Block=true Verdict=FAIL", out.Block, out.Verdict)
	}
	if !strings.Contains(out.Message, "- T001 src/cart.test.ts ") {
		t.Errorf("expected T001 line in Message:\n%s", out.Message)
	}
	if strings.Contains(out.Message, "- H001 src/cart.test.ts ") {
		t.Errorf("expected H001 duplicate to be suppressed in Message:\n%s", out.Message)
	}
	if !strings.Contains(out.Message, "donegate: not done (1 failure, attempt 1/5)") {
		t.Errorf("expected 1 failure in header:\n%s", out.Message)
	}
}
