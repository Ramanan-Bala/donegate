package diff

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Ramanan-Bala/donegate/internal/gitx"
)

// Status represents how a file changed relative to the base commit.
type Status string

const (
	Added    Status = "added"
	Modified Status = "modified"
	Deleted  Status = "deleted"
	Renamed  Status = "renamed"
)

// Line is a single added or removed line with its 1-based line number.
type Line struct {
	No   int
	Text string
}

// File holds the diff metadata and changed lines for one file.
type File struct {
	Path    string
	OldPath string
	Status  Status
	Binary  bool
	Added   []Line
	Removed []Line
}

// Diff represents all changes between a base commit and the working tree.
type Diff struct {
	RepoDir string
	Base    string
	Files   []File
}

// File returns the diff entry for path, or nil if path did not change.
func (d *Diff) File(path string) *File {
	norm := filepath.ToSlash(path)
	for i := range d.Files {
		if d.Files[i].Path == norm {
			return &d.Files[i]
		}
	}
	return nil
}

// ReadBase reads path as it existed in the base commit.
func (d *Diff) ReadBase(path string) ([]byte, error) {
	spec := fmt.Sprintf("%s:%s", d.Base, filepath.ToSlash(path))
	out, err := gitx.Output(d.RepoDir, nil, "show", spec)
	if err != nil {
		return nil, fmt.Errorf("read base %s: %w", path, err)
	}
	return out, nil
}

// ReadHead reads path from the current working tree.
func (d *Diff) ReadHead(path string) ([]byte, error) {
	full := filepath.Join(d.RepoDir, filepath.FromSlash(path))
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, fmt.Errorf("read working tree %s: %w", path, err)
	}
	return data, nil
}

// Load computes the diff between baseRef and the working tree in repoDir.
func Load(repoDir, baseRef string) (*Diff, error) {
	if repoDir == "" {
		repoDir = "."
	}
	shaBytes, err := gitx.Output(repoDir, nil, "rev-parse", "--verify", baseRef+"^{commit}")
	if err != nil {
		return nil, fmt.Errorf("resolve base %q: %w", baseRef, err)
	}
	baseSHA := strings.TrimSpace(string(shaBytes))

	idxEnv, cleanup, err := gitx.TempIndex(repoDir)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	if _, err := gitx.Output(repoDir, idxEnv, "add", "-A"); err != nil {
		return nil, fmt.Errorf("stage working tree: %w", err)
	}

	nameStatusOut, err := gitx.Output(repoDir, idxEnv, "-c", "core.quotepath=false", "diff", "--cached", "--no-ext-diff", "--name-status", "-z", "-M", baseSHA)
	if err != nil {
		return nil, fmt.Errorf("diff name-status against %s: %w", baseSHA, err)
	}

	files, err := parseNameStatus(nameStatusOut)
	if err != nil {
		return nil, fmt.Errorf("parse diff name-status: %w", err)
	}

	for i := range files {
		f := &files[i]

		args := []string{"-c", "core.quotepath=false", "diff", "--cached", "--no-ext-diff", "--no-textconv", "-U0", "--no-color", "-M", baseSHA, "--"}
		if f.Status == Renamed && f.OldPath != "" {
			args = append(args, f.OldPath, f.Path)
		} else {
			args = append(args, f.Path)
		}
		patchOut, err := gitx.Output(repoDir, idxEnv, args...)
		if err != nil {
			return nil, fmt.Errorf("diff lines for %s: %w", f.Path, err)
		}
		applyUnifiedDiff(f, patchOut)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	return &Diff{
		RepoDir: repoDir,
		Base:    baseSHA,
		Files:   files,
	}, nil
}

func parseNameStatus(out []byte) ([]File, error) {
	if len(out) == 0 {
		return nil, nil
	}
	tokens := bytes.Split(out, []byte{0})
	var files []File
	for i := 0; i < len(tokens); {
		if len(tokens[i]) == 0 {
			i++
			continue
		}
		code := string(tokens[i])
		switch {
		case strings.HasPrefix(code, "R"):
			if i+2 >= len(tokens) {
				return nil, fmt.Errorf("truncated rename record for %q", code)
			}
			files = append(files, File{
				OldPath: filepath.ToSlash(string(tokens[i+1])),
				Path:    filepath.ToSlash(string(tokens[i+2])),
				Status:  Renamed,
			})
			i += 3
		case strings.HasPrefix(code, "C"):
			if i+2 >= len(tokens) {
				return nil, fmt.Errorf("truncated copy record for %q", code)
			}
			files = append(files, File{
				OldPath: filepath.ToSlash(string(tokens[i+1])),
				Path:    filepath.ToSlash(string(tokens[i+2])),
				Status:  Added,
			})
			i += 3
		default:
			if i+1 >= len(tokens) {
				return nil, fmt.Errorf("truncated status record for %q", code)
			}
			var st Status
			switch code[0] {
			case 'A':
				st = Added
			case 'D':
				st = Deleted
			case 'M', 'T':
				st = Modified
			default:
				st = Modified
			}
			files = append(files, File{
				Path:   filepath.ToSlash(string(tokens[i+1])),
				Status: st,
			})
			i += 2
		}
	}
	return files, nil
}

func applyUnifiedDiff(f *File, patch []byte) {
	if len(patch) == 0 {
		return
	}
	s := strings.TrimSuffix(string(patch), "\n")
	lines := strings.Split(s, "\n")

	var oldLine, newLine int
	var oldRem, newRem int

	for _, line := range lines {
		if oldRem == 0 && newRem == 0 {
			if strings.HasPrefix(line, "Binary files ") && strings.HasSuffix(line, " differ") {
				f.Binary = true
				return
			}
			if os, oc, ns, nc, ok := parseHunkHeader(line); ok {
				oldLine = os
				oldRem = oc
				newLine = ns
				newRem = nc
			}
			continue
		}

		if line == "" {
			continue
		}
		switch line[0] {
		case '+':
			f.Added = append(f.Added, Line{No: newLine, Text: line[1:]})
			newLine++
			if newRem > 0 {
				newRem--
			}
		case '-':
			f.Removed = append(f.Removed, Line{No: oldLine, Text: line[1:]})
			oldLine++
			if oldRem > 0 {
				oldRem--
			}
		case ' ':
			oldLine++
			newLine++
			if oldRem > 0 {
				oldRem--
			}
			if newRem > 0 {
				newRem--
			}
		case '\\':
			// "\ No newline at end of file" does not consume a line number.
		}
	}
}

func parseHunkHeader(line string) (oldStart, oldCount, newStart, newCount int, ok bool) {
	if !strings.HasPrefix(line, "@@ -") {
		return 0, 0, 0, 0, false
	}
	rest := line[len("@@ -"):]
	end := strings.Index(rest, " @@")
	if end < 0 {
		return 0, 0, 0, 0, false
	}
	header := rest[:end]
	oldPart, newPart, found := strings.Cut(header, " +")
	if !found {
		return 0, 0, 0, 0, false
	}
	var err error
	oldStart, oldCount, err = parseRange(oldPart)
	if err != nil {
		return 0, 0, 0, 0, false
	}
	newStart, newCount, err = parseRange(newPart)
	if err != nil {
		return 0, 0, 0, 0, false
	}
	return oldStart, oldCount, newStart, newCount, true
}

func parseRange(part string) (start, count int, err error) {
	startStr, countStr, hasComma := strings.Cut(part, ",")
	start, err = strconv.Atoi(startStr)
	if err != nil {
		return 0, 0, err
	}
	if !hasComma {
		return start, 1, nil
	}
	count, err = strconv.Atoi(countStr)
	if err != nil {
		return 0, 0, err
	}
	return start, count, nil
}
