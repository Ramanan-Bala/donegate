package session

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Ramanan-Bala/donegate/internal/gitx"
	"github.com/Ramanan-Bala/donegate/internal/policy"
	"gopkg.in/yaml.v3"
)

// ErrNoSession is returned when no active session exists for a repository.
var ErrNoSession = errors.New("no active session")

// Session holds the baseline commit, policy snapshot, and protected-file hashes for a verification session.
type Session struct {
	ID             string            `json:"id"`
	Repo           string            `json:"repo"`
	Head           string            `json:"head"`
	Base           string            `json:"base"`
	Harness        string            `json:"harness"`
	HarnessSession string            `json:"harness_session,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	PolicySHA256   string            `json:"policy_sha256"`
	Protected      map[string]string `json:"protected"`
}

// StartOptions configures starting a new verification session.
type StartOptions struct {
	Harness        string
	HarnessSession string
}

// HashMismatch describes a protected file whose working-tree content differs from the session baseline.
type HashMismatch struct {
	Path     string `json:"path"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

// StateDir returns the donegate state directory ($DONEGATE_HOME or ~/.donegate).
func StateDir() (string, error) {
	if v := os.Getenv("DONEGATE_HOME"); v != "" {
		return filepath.Clean(v), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".donegate"), nil
}

// Start snapshots the repository's current working tree, policy, and protected files into a new session.
func Start(repoDir, harness string) (*Session, error) {
	return StartWith(repoDir, StartOptions{Harness: harness})
}

// StartWith snapshots the repository's current working tree, policy, and protected files into a new session.
func StartWith(repoDir string, opts StartOptions) (*Session, error) {
	repoRoot, err := resolveRepoRoot(repoDir)
	if err != nil {
		return nil, err
	}

	head, err := resolveHead(repoRoot)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	id, err := newSessionID(now)
	if err != nil {
		return nil, fmt.Errorf("generate session id: %w", err)
	}

	pol, policyBytes, hasRepoPolicy, err := snapshotPolicy(repoRoot)
	if err != nil {
		return nil, err
	}
	policySum := sha256.Sum256(policyBytes)
	policySHA := hex.EncodeToString(policySum[:])

	baseSHA, err := createBaselineCommit(repoRoot, head, id)
	if err != nil {
		return nil, err
	}

	protected, err := hashProtectedFiles(repoRoot, baseSHA, pol, hasRepoPolicy)
	if err != nil {
		return nil, err
	}

	sess := &Session{
		ID:             id,
		Repo:           repoRoot,
		Head:           head,
		Base:           baseSHA,
		Harness:        opts.Harness,
		HarnessSession: opts.HarnessSession,
		CreatedAt:      now,
		PolicySHA256:   policySHA,
		Protected:      protected,
	}

	if err := writeSessionState(sess, policyBytes); err != nil {
		return nil, err
	}

	return sess, nil
}

// Load reads a previously saved session by ID from the state directory.
func Load(id string) (*Session, error) {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\") {
		return nil, fmt.Errorf("load session: invalid session id %q", id)
	}
	stateDir, err := StateDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(stateDir, "sessions", id, "session.json"))
	if err != nil {
		return nil, fmt.Errorf("load session %s: %w", id, err)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("decode session %s: %w", id, err)
	}
	if s.Protected == nil {
		s.Protected = make(map[string]string)
	}
	return &s, nil
}

// Current loads the active session for the repository containing repoDir.
func Current(repoDir string) (*Session, error) {
	repoRoot, err := resolveRepoRoot(repoDir)
	if err != nil {
		return nil, err
	}
	stateDir, err := StateDir()
	if err != nil {
		return nil, err
	}
	ptrPath := filepath.Join(stateDir, "repos", repoKey(repoRoot), "current")
	data, err := os.ReadFile(ptrPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNoSession, repoRoot)
		}
		return nil, fmt.Errorf("read current session pointer: %w", err)
	}
	id := strings.TrimSpace(string(data))
	if id == "" {
		return nil, fmt.Errorf("%w: %s", ErrNoSession, repoRoot)
	}
	return Load(id)
}

// Dir returns the directory storing this session's snapshot files.
func (s *Session) Dir() string {
	stateDir, err := StateDir()
	if err != nil {
		return ""
	}
	return filepath.Join(stateDir, "sessions", s.ID)
}

// PolicyBytes returns the raw snapshotted policy.yml stored in the session directory.
func (s *Session) PolicyBytes() ([]byte, error) {
	dir := s.Dir()
	if dir == "" {
		return nil, fmt.Errorf("session policy: resolve session dir")
	}
	data, err := os.ReadFile(filepath.Join(dir, "policy.yml"))
	if err != nil {
		return nil, fmt.Errorf("read snapshot policy: %w", err)
	}
	return data, nil
}

// Policy parses and returns the snapshotted policy.yml stored in the session directory.
func (s *Session) Policy() (*policy.Policy, error) {
	data, err := s.PolicyBytes()
	if err != nil {
		return nil, err
	}
	pol, err := policy.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse snapshot policy: %w", err)
	}
	return pol, nil
}

// CheckProtected re-hashes every protected path in the working tree and returns mismatches sorted by Path.
func (s *Session) CheckProtected() ([]HashMismatch, error) {
	paths := make([]string, 0, len(s.Protected))
	for p := range s.Protected {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var mismatches []HashMismatch
	for _, rel := range paths {
		expected := s.Protected[rel]
		full := filepath.Join(s.Repo, filepath.FromSlash(rel))
		actual, err := hashFile(full)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				mismatches = append(mismatches, HashMismatch{
					Path:     rel,
					Expected: expected,
					Actual:   "",
				})
				continue
			}
			return nil, fmt.Errorf("hash protected file %s: %w", rel, err)
		}
		if actual != expected {
			mismatches = append(mismatches, HashMismatch{
				Path:     rel,
				Expected: expected,
				Actual:   actual,
			})
		}
	}
	return mismatches, nil
}

func resolveRepoRoot(repoDir string) (string, error) {
	if repoDir == "" {
		repoDir = "."
	}
	out, err := gitx.Output(repoDir, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("resolve repo root: %w", err)
	}
	root := strings.TrimSpace(string(out))
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("abs repo root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("eval symlinks repo root: %w", err)
	}
	return resolved, nil
}

func resolveHead(repoRoot string) (string, error) {
	out, err := gitx.Output(repoRoot, nil, "rev-parse", "--verify", "HEAD")
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	if _, symErr := gitx.Output(repoRoot, nil, "symbolic-ref", "--quiet", "HEAD"); symErr == nil {
		return "", nil
	}
	return "", fmt.Errorf("resolve HEAD: %w", err)
}

func newSessionID(now time.Time) (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return now.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b[:]), nil
}

func snapshotPolicy(repoRoot string) (*policy.Policy, []byte, bool, error) {
	repoPolicyPath := filepath.Join(repoRoot, ".donegate.yml")
	data, err := os.ReadFile(repoPolicyPath)
	if err == nil {
		pol, parseErr := policy.Parse(data)
		if parseErr != nil {
			return nil, nil, false, fmt.Errorf("load repo policy: %w", parseErr)
		}
		return pol, data, true, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, false, fmt.Errorf("read repo policy: %w", err)
	}

	pol := policy.Default()
	defaultBytes, err := yaml.Marshal(pol)
	if err != nil {
		return nil, nil, false, fmt.Errorf("marshal default policy: %w", err)
	}
	return pol, defaultBytes, false, nil
}

func createBaselineCommit(repoRoot, head, id string) (string, error) {
	idxEnv, cleanup, err := gitx.TempIndex(repoRoot)
	if err != nil {
		return "", err
	}
	defer cleanup()

	if _, err := gitx.Output(repoRoot, idxEnv, "add", "-A"); err != nil {
		return "", fmt.Errorf("stage working tree: %w", err)
	}
	treeOut, err := gitx.Output(repoRoot, idxEnv, "write-tree")
	if err != nil {
		return "", fmt.Errorf("write baseline tree: %w", err)
	}
	treeSHA := strings.TrimSpace(string(treeOut))

	commitEnv := []string{
		"GIT_AUTHOR_NAME=donegate",
		"GIT_AUTHOR_EMAIL=donegate@localhost",
		"GIT_COMMITTER_NAME=donegate",
		"GIT_COMMITTER_EMAIL=donegate@localhost",
	}
	commitArgs := []string{"-c", "commit.gpgsign=false", "commit-tree", treeSHA, "-m", "donegate session " + id}
	if head != "" {
		commitArgs = []string{"-c", "commit.gpgsign=false", "commit-tree", treeSHA, "-p", head, "-m", "donegate session " + id}
	}
	baseOut, err := gitx.Output(repoRoot, commitEnv, commitArgs...)
	if err != nil {
		return "", fmt.Errorf("commit baseline tree: %w", err)
	}
	baseSHA := strings.TrimSpace(string(baseOut))

	if _, err := gitx.Output(repoRoot, nil, "update-ref", "refs/donegate/sessions/"+id, baseSHA); err != nil {
		return "", fmt.Errorf("pin baseline ref: %w", err)
	}
	return baseSHA, nil
}

func hashProtectedFiles(repoRoot, baseSHA string, pol *policy.Policy, hasRepoPolicy bool) (map[string]string, error) {
	out, err := gitx.Output(repoRoot, nil, "-c", "core.quotepath=false", "ls-tree", "-r", "--name-only", "-z", baseSHA)
	if err != nil {
		return nil, fmt.Errorf("list baseline files: %w", err)
	}

	protected := make(map[string]string)
	for _, tok := range bytes.Split(out, []byte{0}) {
		if len(tok) == 0 {
			continue
		}
		rel := filepath.ToSlash(string(tok))
		if pol.IsTest(rel) || pol.IsConfig(rel) {
			sum, err := hashFile(filepath.Join(repoRoot, filepath.FromSlash(rel)))
			if err != nil {
				return nil, fmt.Errorf("hash protected file %s: %w", rel, err)
			}
			protected[rel] = sum
		}
	}

	if hasRepoPolicy {
		sum, err := hashFile(filepath.Join(repoRoot, ".donegate.yml"))
		if err != nil {
			return nil, fmt.Errorf("hash protected file .donegate.yml: %w", err)
		}
		protected[".donegate.yml"] = sum
	}

	return protected, nil
}

func writeSessionState(sess *Session, policyBytes []byte) error {
	stateDir, err := StateDir()
	if err != nil {
		return err
	}
	sessDir := filepath.Join(stateDir, "sessions", sess.ID)
	if err := os.MkdirAll(sessDir, 0o700); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, "policy.yml"), policyBytes, 0o600); err != nil {
		return fmt.Errorf("write snapshot policy: %w", err)
	}

	sessBytes, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session: %w", err)
	}
	sessBytes = append(sessBytes, '\n')
	if err := os.WriteFile(filepath.Join(sessDir, "session.json"), sessBytes, 0o600); err != nil {
		return fmt.Errorf("write session file: %w", err)
	}

	repoStateDir := filepath.Join(stateDir, "repos", repoKey(sess.Repo))
	if err := os.MkdirAll(repoStateDir, 0o700); err != nil {
		return fmt.Errorf("create repo state dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(repoStateDir, "current"), []byte(sess.ID+"\n"), 0o600); err != nil {
		return fmt.Errorf("write current session pointer: %w", err)
	}
	return nil
}

func repoKey(repoRoot string) string {
	sum := sha256.Sum256([]byte(repoRoot))
	return hex.EncodeToString(sum[:])[:16]
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
