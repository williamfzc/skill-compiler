// Package paths is the filesystem foundation: small, stable path helpers with
// no product opinion. Everything else leans on these; this package leans on
// nothing above it.
package paths

import (
	"os"
	"path/filepath"
	"strings"
)

// Prune lists directory names never worth descending into while scanning.
var Prune = map[string]bool{
	"node_modules": true, ".git": true, "venv": true, ".venv": true,
	"__pycache__": true, "dist": true, "build": true, "target": true,
	".next": true, ".gradle": true, "Pods": true, ".idea": true, ".tox": true,
	"vendor": true, "tmp": true, "temp": true, ".cache": true, "backup": true,
	".trash": true, "Carthage": true, ".mypy_cache": true, ".pytest_cache": true,
	"site-packages": true, ".terraform": true,
	// "testdata" is fixture data by tooling convention (the Go tool ignores
	// it): scaffolding under it is test content, never a skill an agent loads.
	"testdata": true,
}

// Expand returns an absolute, user-expanded path.
func Expand(p string) string {
	if p == "~" {
		if h, err := os.UserHomeDir(); err == nil {
			p = h
		}
	} else if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(h, p[2:])
		}
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// ReadBytes reads a file, tolerating errors; nil on any failure.
func ReadBytes(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return b
}

// ReadText reads a file as a UTF-8 string; "" on any failure.
func ReadText(path string) string {
	return string(ReadBytes(path))
}

// Shorten collapses the home directory to ~ for display. The replacement is
// prefix-anchored at a path boundary: a naive substring replace mangles paths
// whose realpath sits below a symlinked $HOME prefix (on macOS /tmp is
// /private/tmp, so a realpathed path contains the home string after an extra
// "/private" segment and would collapse to "/private~/...").
func Shorten(p string) string {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return p
	}
	if rel, ok := relTo(p, h); ok {
		return "~" + rel
	}
	// $HOME itself may cross a symlink (realpath differs from the env var);
	// compare against the resolved form too.
	if hr := RealPath(h); hr != h {
		if rel, ok := relTo(p, hr); ok {
			return "~" + rel
		}
	}
	return p
}

// relTo returns the separator-prefixed path of p relative to base when p
// equals base or starts with base + separator; ok=false otherwise.
func relTo(p, base string) (string, bool) {
	if p == base {
		return "", true
	}
	if strings.HasPrefix(p, base+string(os.PathSeparator)) {
		return p[len(base):], true
	}
	return "", false
}

// Exists reports whether the path is reachable (following symlinks).
func Exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// RealPath lexically resolves every symlink component of a path, including a
// missing tail (a broken symlink is still an observed fact with a target).
// This mirrors Python's os.path.realpath rather than filepath.EvalSymlinks,
// which gives up at the first non-existent component. Symlink targets are
// spliced back into the component queue so that links inside them resolve too
// (on macOS /var is itself a symlink to /private/var).
func RealPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	vol := filepath.VolumeName(abs)
	rest := abs[len(vol):]
	parts := strings.Split(rest, string(os.PathSeparator))
	cur := vol
	if cur == "" {
		cur = "/"
	}
	guard := 0
	for len(parts) > 0 {
		comp := parts[0]
		parts = parts[1:]
		if comp == "" || comp == "." {
			continue
		}
		if comp == ".." {
			cur = filepath.Dir(cur)
			continue
		}
		next := filepath.Join(cur, comp)
		if t, err := os.Readlink(next); err == nil {
			var targetParts []string
			if filepath.IsAbs(t) {
				cur = "/"
				targetParts = strings.Split(t, string(os.PathSeparator))
			} else {
				targetParts = strings.Split(t, string(os.PathSeparator))
			}
			parts = append(targetParts, parts...)
			guard++
			if guard > 256 {
				return next
			}
		} else {
			cur = next
		}
	}
	return cur
}

// ReadDirNames returns a directory's entries in raw filesystem order (not
// sorted -- the order the compiler records provenance in must match what the
// loader actually sees).
func ReadDirNames(dir string) []string {
	f, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		return nil
	}
	return names
}

// WalkFollow walks root depth-first in raw readdir order, descending into
// symlinked directories. Each real directory is descended at most once
// (symlink-cycle guard), but fn is invoked for every arrival path, so every
// mount point is observed. Names in Prune are never descended. fn returning
// stop halts the walk.
func WalkFollow(root string, fn func(dir string, names []string) (stop bool)) {
	descended := map[string]bool{}
	stack := []string{root}
	for len(stack) > 0 {
		dir := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		names := ReadDirNames(dir)
		if names == nil {
			continue
		}
		if fn(dir, names) {
			return
		}
		rp := RealPath(dir)
		if descended[rp] {
			continue
		}
		descended[rp] = true
		// Push in reverse so raw readdir order is preserved (LIFO stack).
		for i := len(names) - 1; i >= 0; i-- {
			name := names[i]
			if Prune[name] {
				continue
			}
			full := filepath.Join(dir, name)
			info, err := os.Lstat(full)
			if err != nil {
				continue
			}
			if info.Mode()&os.ModeSymlink != 0 {
				t, err := os.Stat(full)
				if err != nil || !t.IsDir() {
					continue
				}
			} else if !info.IsDir() {
				continue
			}
			stack = append(stack, full)
		}
	}
}

// FindRepoRoots returns every ancestor of start that holds a .git, nearest
// first.
func FindRepoRoots(start string) []string {
	var roots []string
	cur := Expand(start)
	for {
		if Exists(filepath.Join(cur, ".git")) {
			roots = append(roots, cur)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return roots
		}
		cur = parent
	}
}
