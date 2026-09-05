// Package refparse extracts reference targets from markdown documents.
//
// Extraction is AST-based (goldmark, the CommonMark standard) so code
// exclusion belongs to the parser, not to regex fence-tracking: fenced code
// and HTML blocks are skipped by the parser itself. Link and image
// destinations come from the AST; a narrow regex still catches bare relative
// paths written in plain prose, which are not link syntax.
//
// Two tiers of targets come out: prose targets (Quoted=false) and inline-code
// candidates (Quoted=true). A quoted candidate counts only if it resolves --
// rustdoc treats backticks as links too; an unresolvable one (an example
// filename like TODO.md) stays silent. Deciding "resolves" needs the
// filesystem, so it belongs to the caller via Resolve.
package refparse

import (
	"bytes"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"skillscope/internal/frontmatter"
	"skillscope/internal/paths"
)

// Target is one reference candidate found in a markdown document.
type Target struct {
	Raw    string // the link or path as written
	Quoted bool   // found inside inline code; counts only when it resolves
}

// reBare matches a relative path (./ or ../ prefix) with a known extension in
// plain prose. reSuite matches suite-style skills/<name>/SKILL.md paths.
// reQuoted matches any relative path shape with a known extension inside
// inline code; quoted candidates count only when they resolve, so a wider
// shape is safe there.
var reBare = regexp.MustCompile(`(?:^|[^\w` + "`" + `])(\.\.?/[A-Za-z0-9_./-]+\.(?:md|sh|py|json|ya?ml|txt))`)
var reSuite = regexp.MustCompile(`(?:^|[\s` + "`" + `(|])((?:[A-Za-z0-9_.-]+/)*skills/[A-Za-z0-9_.-]+/SKILL\.md)`)
var reQuoted = regexp.MustCompile(`[A-Za-z0-9_][A-Za-z0-9_.@+-]*(?:/[A-Za-z0-9_.@+-]+)*\.(?:md|sh|py|json|ya?ml|txt)`)
var reScheme = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)

// Extract returns the reference candidates a markdown document contains,
// prose targets and quoted candidates deduped by raw value (prose wins).
func Extract(content []byte) []Target {
	body := []byte(frontmatter.Body(string(content)))
	doc := goldmark.New().Parser().Parse(text.NewReader(body))

	prose := map[string]bool{}
	quoted := map[string]bool{}
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.FencedCodeBlock, *ast.CodeBlock, *ast.HTMLBlock:
			return ast.WalkSkipChildren, nil // samples, not references
		case *ast.Link:
			addDest(prose, string(v.Destination))
			return ast.WalkSkipChildren, nil
		case *ast.Image:
			addDest(prose, string(v.Destination))
			return ast.WalkSkipChildren, nil
		case *ast.AutoLink:
			addDest(prose, string(v.URL(body)))
			return ast.WalkSkipChildren, nil
		case *ast.CodeSpan:
			for _, m := range reQuoted.FindAllString(codeSpanText(v, body), -1) {
				quoted[m] = true
			}
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			plain := string(v.Value(body))
			for _, m := range reBare.FindAllStringSubmatch(plain, -1) {
				prose[m[1]] = true
			}
			for _, m := range reSuite.FindAllStringSubmatch(plain, -1) {
				prose[m[1]] = true
			}
			return ast.WalkContinue, nil
		}
		return ast.WalkContinue, nil
	})

	var out []Target
	for raw := range prose {
		out = append(out, Target{Raw: raw})
	}
	for raw := range quoted {
		if !prose[raw] {
			out = append(out, Target{Raw: raw, Quoted: true})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Raw < out[j].Raw })
	return out
}

func addDest(prose map[string]bool, dest string) {
	dest = strings.TrimSpace(dest)
	if dest == "" || reScheme.MatchString(dest) || strings.ContainsAny(dest, " \t") {
		return // anchors, URLs, and spaced destinations never resolve locally
	}
	prose[dest] = true
}

// codeSpanText joins a code span's child text segments.
func codeSpanText(span *ast.CodeSpan, source []byte) string {
	var b bytes.Buffer
	for c := span.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			b.Write(t.Value(source))
		}
	}
	return b.String()
}

// Resolve resolves one raw link to an existing path, or "". It tries
// file-relative first, then skill-root-relative, then repo-root-relative
// (skills write links all three ways). It returns the path component (anchors
// and queries stripped) and the resolved path.
func Resolve(raw, fromDir, src string, repoRoots []string) (string, string) {
	pp := raw
	if i := strings.Index(pp, "#"); i >= 0 {
		pp = pp[:i]
	}
	if i := strings.Index(pp, "?"); i >= 0 {
		pp = pp[:i]
	}
	if pp == "" || strings.HasSuffix(pp, "/") {
		return pp, ""
	}
	resolved := filepath.Clean(filepath.Join(fromDir, pp))
	if !paths.Exists(resolved) && !strings.HasPrefix(pp, "..") {
		alt1 := filepath.Clean(filepath.Join(src, pp))
		if paths.Exists(alt1) {
			return pp, alt1
		}
		for _, rr := range repoRoots {
			alt := filepath.Clean(filepath.Join(rr, pp))
			if paths.Exists(alt) {
				return pp, alt
			}
		}
	}
	if paths.Exists(resolved) {
		return pp, resolved
	}
	return pp, ""
}
