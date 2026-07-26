// Package rename implements the pattern language behind the Multi-Rename
// Tool (Ctrl+M) — TotalCmd-style batch renaming with name/extension
// placeholders, a counter, case conversion, and find/replace (plain text or
// regex). It has no Fyne dependency and is unit-tested directly, matching
// internal/fsops's own convention; multirename_ui.go is the Fyne-facing
// half (the dialog, live preview, and the actual on-disk renames).
package rename

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// CaseMode is a whole-name case transform, applied after pattern expansion
// and before find/replace.
type CaseMode int

const (
	CaseNone CaseMode = iota
	CaseUpper
	CaseLower
	CaseTitle
	CaseSentence
)

// Options is one Multi-Rename Tool configuration, applied to every
// selected item (see PreviewBatch).
type Options struct {
	// Pattern is the new-name template. Empty defaults to "[N]" (keep the
	// original base name) so Case/Find/Replace can be used on their own
	// without retyping it. Placeholders:
	//   [N]         original base name (without extension)
	//   [N<a>-<b>]  characters a through b of the base name (1-indexed, inclusive)
	//   [E]         original extension (without the dot)
	//   [C]         counter, start 1 step 1 no padding
	//   [C:s]       counter starting at s
	//   [C:s,step]  counter starting at s, incrementing by step
	//   [C:s,step,w] counter, zero-padded to width w
	// If the pattern doesn't reference [E], the original extension is
	// reattached unchanged — so a plain "[N]"-style pattern never
	// accidentally drops it.
	Pattern  string
	Case     CaseMode
	Find     string
	Replace  string
	UseRegex bool
}

var (
	nRangeRe = regexp.MustCompile(`\[N(\d+)-(\d+)\]`)
	nRe      = regexp.MustCompile(`\[N\]`)
	eRe      = regexp.MustCompile(`\[E\]`)
	cRe      = regexp.MustCompile(`\[C(?::(-?\d+)(?:,(-?\d+))?(?:,(\d+))?)?\]`)
)

// Preview computes the new name for name, the index-th (1-based) item in
// its batch — index feeds the [C] counter.
func Preview(opts Options, name string, index int) (string, error) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	extNoDot := strings.TrimPrefix(ext, ".")

	pattern := opts.Pattern
	if strings.TrimSpace(pattern) == "" {
		pattern = "[N]"
	}
	includesE := strings.Contains(pattern, "[E]")

	result := nRangeRe.ReplaceAllStringFunc(pattern, func(tok string) string {
		m := nRangeRe.FindStringSubmatch(tok)
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		return substrRange(stem, a, b)
	})
	result = nRe.ReplaceAllString(result, stem)
	result = eRe.ReplaceAllString(result, extNoDot)
	result = cRe.ReplaceAllStringFunc(result, func(tok string) string {
		return counterValue(cRe.FindStringSubmatch(tok), index)
	})

	result = applyCase(result, opts.Case)

	result, err := applyFindReplace(result, opts.Find, opts.Replace, opts.UseRegex)
	if err != nil {
		return "", err
	}

	if !includesE && ext != "" {
		result += ext
	}
	if result == "" {
		return "", errors.New("pattern produces an empty name")
	}
	return result, nil
}

// PreviewBatch computes new names for every entry in names, in order —
// index (for [C]) is each item's 1-based position in this slice.
func PreviewBatch(opts Options, names []string) ([]string, error) {
	out := make([]string, len(names))
	for i, n := range names {
		newName, err := Preview(opts, n, i+1)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		out[i] = newName
	}
	return out, nil
}

func substrRange(s string, a, b int) string {
	r := []rune(s)
	if a < 1 {
		a = 1
	}
	if b > len(r) {
		b = len(r)
	}
	if a > b || a > len(r) {
		return ""
	}
	return string(r[a-1 : b])
}

func counterValue(m []string, index int) string {
	start, step, width := 1, 1, 1
	if m[1] != "" {
		start, _ = strconv.Atoi(m[1])
	}
	if m[2] != "" {
		step, _ = strconv.Atoi(m[2])
	}
	if m[3] != "" {
		width, _ = strconv.Atoi(m[3])
	}
	n := start + (index-1)*step
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	for len(s) < width {
		s = "0" + s
	}
	if neg {
		s = "-" + s
	}
	return s
}

func applyCase(s string, mode CaseMode) string {
	switch mode {
	case CaseUpper:
		return strings.ToUpper(s)
	case CaseLower:
		return strings.ToLower(s)
	case CaseTitle:
		return toTitleCase(s)
	case CaseSentence:
		return toSentenceCase(s)
	default:
		return s
	}
}

// toTitleCase upper-cases the first letter of each word (splitting on
// space/underscore/hyphen/dot) and lower-cases the rest — Go's own
// strings.Title is deprecated and this avoids a new dependency for it.
func toTitleCase(s string) string {
	var b strings.Builder
	newWord := true
	for _, r := range s {
		switch {
		case newWord && unicode.IsLetter(r):
			b.WriteRune(unicode.ToUpper(r))
			newWord = false
		default:
			b.WriteRune(unicode.ToLower(r))
		}
		if r == ' ' || r == '_' || r == '-' || r == '.' {
			newWord = true
		}
	}
	return b.String()
}

func toSentenceCase(s string) string {
	r := []rune(strings.ToLower(s))
	if len(r) == 0 {
		return s
	}
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func applyFindReplace(s, find, replace string, useRegex bool) (string, error) {
	if find == "" {
		return s, nil
	}
	if useRegex {
		re, err := regexp.Compile(find)
		if err != nil {
			return "", fmt.Errorf("invalid regex: %w", err)
		}
		return re.ReplaceAllString(s, replace), nil
	}
	return strings.ReplaceAll(s, find, replace), nil
}
