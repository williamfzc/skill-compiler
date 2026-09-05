// Package frontmatter reads a SKILL.md's YAML frontmatter.
//
// A skill's loadability is decided by its frontmatter, so parsing it is a
// foundation concern kept apart from how nodes are built. Deliberately minimal:
// top-level keys only, enough to see whether name/description exist and to spot
// structural damage.
package frontmatter

import (
	"regexp"
	"strings"
)

var reKey = regexp.MustCompile(`^([A-Za-z0-9_-]+)\s*:(.*)$`)

var blockMarkers = map[string]bool{
	"|": true, ">": true, "|-": true, ">-": true, "|+": true, ">+": true,
}

// Body returns the document with a leading frontmatter block removed, so
// reference extraction sees only the prose an agent reads as the document
// body; frontmatter is structured data parsed separately.
func Body(text string) string {
	if !strings.HasPrefix(text, "---") {
		return text
	}
	parts := strings.SplitN(text, "---", 3)
	if len(parts) < 3 {
		return text
	}
	return strings.TrimLeft(parts[2], "\n")
}

// Parse returns (fields, hasFrontmatter). Top-level keys only; folded/literal
// scalar blocks (|, >, and their chomp variants) are joined into one string.
func Parse(text string) (map[string]string, bool) {
	if !strings.HasPrefix(text, "---") {
		return map[string]string{}, false
	}
	parts := strings.SplitN(text, "---", 3)
	if len(parts) < 3 {
		return map[string]string{}, false
	}
	fm := map[string]string{}
	key := ""
	var buf []string
	flush := func() {
		if key != "" && len(buf) > 0 {
			fm[key] = strings.Join(buf, " ")
		}
		buf = nil
	}
	for _, line := range strings.Split(parts[1], "\n") {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if key != "" {
				buf = append(buf, strings.TrimSpace(line))
			}
			continue
		}
		m := reKey.FindStringSubmatch(line)
		if m != nil {
			flush()
			key = m[1]
			v := strings.TrimSpace(m[2])
			if v != "" && !blockMarkers[v] {
				fm[key] = strings.Trim(strings.TrimSpace(v), `"'`)
				key = ""
			} else {
				buf = nil
			}
		}
	}
	flush()
	return fm, true
}
