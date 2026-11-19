package warp

import (
	"path"
	"strings"
)

// matchSandboxGlob reports whether name matches the slash-separated glob
// pattern. Each segment is matched with path.Match semantics ("*", "?",
// "[...]"), and a segment consisting solely of "**" matches zero or more whole
// segments. Absolute patterns only match absolute names and relative patterns
// only match relative names. Malformed patterns never match.
func matchSandboxGlob(pattern, name string) bool {
	if pattern == "" {
		return false
	}
	if strings.HasPrefix(pattern, "/") != strings.HasPrefix(name, "/") {
		return false
	}
	patSegs := splitGlobSegments(pattern)
	nameSegs := splitGlobSegments(name)
	return matchGlobSegments(patSegs, nameSegs)
}

// splitGlobSegments splits a slash-separated path into its non-empty segments.
func splitGlobSegments(s string) []string {
	raw := strings.Split(s, "/")
	out := raw[:0]
	for _, seg := range raw {
		if seg != "" {
			out = append(out, seg)
		}
	}
	return out
}

func matchGlobSegments(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			// Collapse consecutive "**" segments.
			rest := pat[1:]
			for len(rest) > 0 && rest[0] == "**" {
				rest = rest[1:]
			}
			if len(rest) == 0 {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if matchGlobSegments(rest, name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		ok, err := path.Match(pat[0], name[0])
		if err != nil || !ok {
			return false
		}
		pat = pat[1:]
		name = name[1:]
	}
	return len(name) == 0
}

// validSandboxGlob reports whether every segment of pattern is a well-formed
// path.Match pattern.
func validSandboxGlob(pattern string) bool {
	if strings.TrimSpace(pattern) == "" {
		return false
	}
	for _, seg := range splitGlobSegments(pattern) {
		if seg == "**" {
			continue
		}
		if _, err := path.Match(seg, ""); err != nil {
			return false
		}
	}
	return true
}
