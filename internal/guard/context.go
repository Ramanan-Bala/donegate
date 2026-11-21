package guard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Ramanan-Bala/donegate/internal/gitx"
	"github.com/Ramanan-Bala/donegate/internal/policy"
	"github.com/Ramanan-Bala/donegate/internal/session"
)

// LoadContext resolves the guard Context for cwd. When an active session exists,
// it uses the session's snapshotted policy and protected baseline without reading
// the working-tree policy. When no session exists, it falls back to git rev-parse
// and the working-tree .donegate.yml (or policy.Default()).
func LoadContext(cwd string) (Context, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Context{}, fmt.Errorf("resolve home dir: %w", err)
	}
	stateDir, err := session.StateDir()
	if err != nil {
		return Context{}, fmt.Errorf("resolve state dir: %w", err)
	}

	dir := cwd
	if dir == "" {
		dir = "."
	}

	sess, err := session.Current(dir)
	if err == nil {
		pol, polErr := sess.Policy()
		if polErr != nil {
			return Context{}, fmt.Errorf("load session policy: %w", polErr)
		}
		return Context{
			RepoRoot:  sess.Repo,
			Policy:    pol,
			Protected: sess.Protected,
			StateDir:  stateDir,
			Home:      home,
		}, nil
	}

	repoRoot, gitErr := resolveGitRoot(dir)
	if !errors.Is(err, session.ErrNoSession) {
		if gitErr != nil {
			// cwd is not inside a git repository: only rules a and b apply.
			return Context{
				RepoRoot:  "",
				Policy:    policy.Default(),
				Protected: nil,
				StateDir:  stateDir,
				Home:      home,
			}, nil
		}
		return Context{}, fmt.Errorf("load current session: %w", err)
	}

	if gitErr != nil {
		return Context{
			RepoRoot:  "",
			Policy:    policy.Default(),
			Protected: nil,
			StateDir:  stateDir,
			Home:      home,
		}, nil
	}

	pol, err := loadWorkingTreePolicy(repoRoot)
	if err != nil {
		return Context{}, err
	}

	return Context{
		RepoRoot:  repoRoot,
		Policy:    pol,
		Protected: nil,
		StateDir:  stateDir,
		Home:      home,
	}, nil
}

func resolveGitRoot(dir string) (string, error) {
	out, err := gitx.Output(dir, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
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

func loadWorkingTreePolicy(repoRoot string) (*policy.Policy, error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, ".donegate.yml"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return policy.Default(), nil
		}
		return nil, fmt.Errorf("read repo policy: %w", err)
	}
	pol, err := policy.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("load repo policy: %w", err)
	}
	return pol, nil
}
