package rules

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/Ramanan-Bala/donegate/internal/diff"
)

func init() {
	Register(s001{})
}

type s001Literal struct {
	key     string
	display string
}

type s001Origin struct {
	display string
	file    string
}

type s001 struct{}

func (s001) ID() string                { return "S001" }
func (s001) Level() int                { return 1 }
func (s001) DefaultSeverity() Severity { return Warn }

func (s001) Check(c *Context) []Finding {
	if c == nil || c.Diff == nil || c.Policy == nil {
		return nil
	}
	if c.Diff.RepoDir == "" || c.Diff.Base == "" {
		return nil
	}

	var candidateFiles []diff.File
	for _, f := range c.Diff.Files {
		if f.Status == diff.Deleted || f.Binary || len(f.Added) == 0 {
			continue
		}
		if isSourceFile(c.Policy, f.Path) {
			candidateFiles = append(candidateFiles, f)
		}
	}
	if len(candidateFiles) == 0 {
		return nil
	}

	treePaths, err := listBaseTree(c.Diff.RepoDir, c.Diff.Base)
	if err != nil {
		return nil
	}
	sort.Strings(treePaths)

	testLiterals := map[string]s001Origin{}
	for _, p := range treePaths {
		if !c.Policy.IsTest(p) {
			continue
		}
		data, err := c.Diff.ReadBase(p)
		if err != nil {
			continue
		}
		for _, lit := range s001ExtractLiterals(string(data)) {
			if _, exists := testLiterals[lit.key]; !exists {
				testLiterals[lit.key] = s001Origin{
					display: lit.display,
					file:    p,
				}
			}
		}
	}
	if len(testLiterals) == 0 {
		return nil
	}

	var findings []Finding
	for _, f := range candidateFiles {
		baseLiterals := map[string]struct{}{}
		if f.Status == diff.Modified || f.Status == diff.Renamed {
			basePath := f.Path
			if f.Status == diff.Renamed && f.OldPath != "" {
				basePath = f.OldPath
			}
			if baseData, err := c.Diff.ReadBase(basePath); err == nil {
				for _, lit := range s001ExtractLiterals(string(baseData)) {
					baseLiterals[lit.key] = struct{}{}
				}
			}
		}

		for _, line := range f.Added {
			for _, lit := range s001ExtractLiterals(line.Text) {
				if _, inBase := baseLiterals[lit.key]; inBase {
					continue
				}
				if origin, ok := testLiterals[lit.key]; ok {
					findings = append(findings, Finding{
						RuleID:   "S001",
						Severity: Warn,
						Path:     f.Path,
						Line:     line.No,
						Message:  fmt.Sprintf("literal %s matches test literal from %s in %s:%d", lit.display, origin.file, f.Path, line.No),
						Hint:     "implement the general logic instead of hardcoding test fixture values",
					})
					break
				}
			}
		}
	}
	return findings
}

func s001ExtractLiterals(src string) []s001Literal {
	var out []s001Literal
	masked, strLits := s001ExtractStringsAndMask(src)
	out = append(out, strLits...)
	out = append(out, s001ScanNumbers(masked)...)
	return out
}

func s001ExtractStringsAndMask(src string) (string, []s001Literal) {
	buf := []byte(src)
	var lits []s001Literal
	for i := 0; i < len(buf); i++ {
		q := buf[i]
		if q != '\'' && q != '"' {
			continue
		}
		start := i
		j := i + 1
		closed := false
		for j < len(buf) {
			if buf[j] == '\n' || buf[j] == '\r' {
				break
			}
			if buf[j] == '\\' {
				j += 2
				continue
			}
			if buf[j] == q {
				closed = true
				break
			}
			j++
		}
		if !closed {
			continue
		}
		inner := string(buf[start+1 : j])
		if len(inner) >= 5 && s001HasDigit(inner) {
			lits = append(lits, s001Literal{
				key:     "str:" + inner,
				display: string(buf[start : j+1]),
			})
		}
		for k := start; k <= j; k++ {
			buf[k] = ' '
		}
		i = j
	}
	return string(buf), lits
}

func s001HasDigit(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			return true
		}
	}
	return false
}

func s001ScanNumbers(s string) []s001Literal {
	var lits []s001Literal
	n := len(s)
	for i := 0; i < n; {
		isNeg := false
		start := i
		if s[i] == '-' {
			if i+1 >= n || !s001IsDigit(s[i+1]) {
				i++
				continue
			}
			if i > 0 && !s001IsUnaryPrev(s[i-1]) {
				i++
				continue
			}
			isNeg = true
			i++
		} else if s001IsDigit(s[i]) {
			if i > 0 && s001IsIdentOrDot(s[i-1]) {
				i++
				continue
			}
		} else {
			i++
			continue
		}

		j := i
		for j < n && (s001IsDigit(s[j]) || s[j] == '_') {
			j++
		}
		hasDot := false
		if j < n && s[j] == '.' && j+1 < n && s001IsDigit(s[j+1]) {
			hasDot = true
			j++
			for j < n && (s001IsDigit(s[j]) || s[j] == '_') {
				j++
			}
		}
		if j < n && s001IsIdentOrDot(s[j]) {
			for j < n && s001IsIdentOrDot(s[j]) {
				j++
			}
			i = j
			continue
		}

		raw := s[start:j]
		clean := strings.ReplaceAll(raw, "_", "")
		if hasDot {
			if s001CountSigDigits(clean, isNeg) >= 4 {
				if val, err := strconv.ParseFloat(clean, 64); err == nil && !math.IsNaN(val) && !math.IsInf(val, 0) {
					if val == 0 {
						val = 0
					}
					lits = append(lits, s001Literal{
						key:     "num:" + strconv.FormatFloat(val, 'g', -1, 64),
						display: raw,
					})
				}
			}
		} else {
			if val, err := strconv.ParseInt(clean, 10, 64); err == nil {
				abs := val
				if abs < 0 {
					abs = -abs
				}
				if val == math.MinInt64 || abs >= 10000 {
					lits = append(lits, s001Literal{
						key:     "num:" + strconv.FormatInt(val, 10),
						display: raw,
					})
				}
			} else if fval, ferr := strconv.ParseFloat(clean, 64); ferr == nil && !math.IsNaN(fval) && !math.IsInf(fval, 0) {
				lits = append(lits, s001Literal{
					key:     "num:" + strconv.FormatFloat(fval, 'g', -1, 64),
					display: raw,
				})
			}
		}
		i = j
	}
	return lits
}

func s001CountSigDigits(clean string, isNeg bool) int {
	digits := clean
	if isNeg {
		digits = digits[1:]
	}
	digits = strings.ReplaceAll(digits, ".", "")
	digits = strings.TrimLeft(digits, "0")
	return len(digits)
}

func s001IsDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func s001IsAlnum(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func s001IsIdentOrDot(b byte) bool {
	return s001IsAlnum(b) || b == '_' || b == '.'
}

func s001IsUnaryPrev(b byte) bool {
	return !s001IsAlnum(b) && b != '_' && b != '.' && b != ')' && b != ']'
}
