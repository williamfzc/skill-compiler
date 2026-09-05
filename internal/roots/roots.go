// Package roots discovers the directories an agent actually loads skills from.
//
// The compiler must reflect what an agent *loads*, not every skill on disk.
// That is two things: the well-known agent skill directories, and the plugin
// cache where only the newest version of each plugin is live. This package owns
// that distinction and nothing else.
package roots

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"skillscope/internal/collect"
	"skillscope/internal/paths"
)

// AgentRootCandidates are the directories an agent loads skills directly from
// (recursively). Skills inside project repositories are intentionally absent
// -- they do not occupy the agent's resident context and are "for later".
//
// The list mirrors the global skills dirs of the agent table in
// vercel-labs/skills (MIT), src/agents.ts @ 435076e (2026-08-18) -- that table
// is the community-maintained source of truth for "which agent loads from
// where"; re-sync against a pinned revision when new agents appear. Divergence
// from upstream is intentional and recorded in docs/load-roots.md:
//   - ~/.trae/skills/.system and ~/.aipaas/skills are observed on real
//     machines but absent upstream;
//   - env-relocated homes (CODEX_HOME etc.) live in envAgentHomes below;
//   - upstream's XDG configHome variants are pinned to ~/.config here.
var AgentRootCandidates = []string{
	"~/.trae/skills",
	"~/.trae-cn/skills",
	"~/.trae/skills/.system",
	"~/.agents/skills",
	"~/.claude/skills",
	"~/.codex/skills",
	"~/.cursor/skills",
	"~/.gemini/skills",
	"~/.gemini/antigravity/skills",
	"~/.gemini/antigravity-cli/skills",
	"~/.aipaas/skills",
	"~/.zcode/skills",
	"~/.config/agents/skills",
	"~/.config/devin/skills",
	"~/.config/goose/skills",
	"~/.config/opencode/skills",
	"~/.config/crush/skills",
	"~/.config/kimchi/harness/skills",
	"~/.aider-desk/skills",
	"~/.augment/skills",
	"~/.bob/skills",
	"~/.codeartsdoer/skills",
	"~/.codebuddy/skills",
	"~/.codemaker/skills",
	"~/.codestudio/skills",
	"~/.codeium/windsurf/skills",
	"~/.commandcode/skills",
	"~/.continue/skills",
	"~/.copilot/skills",
	"~/.deepagents/agent/skills",
	"~/.factory/skills",
	"~/.firebender/skills",
	"~/.forge/skills",
	"~/.iflow/skills",
	"~/.inferencesh/skills",
	"~/.jazz/skills",
	"~/.junie/skills",
	"~/.kilocode/skills",
	"~/.kiro/skills",
	"~/.kode/skills",
	"~/.lingma/skills",
	"~/.mcpjam/skills",
	"~/.minimax/skills",
	"~/.moxby/skills",
	"~/.mux/skills",
	"~/.neovate/skills",
	"~/.ona/skills",
	"~/.openhands/skills",
	"~/.pi/agent/skills",
	"~/.pochi/skills",
	"~/.posit/assistant/skills",
	"~/.qoder/skills",
	"~/.qoder-cn/skills",
	"~/.qwen/skills",
	"~/.reasonix/skills",
	"~/.rovodev/skills",
	"~/.roo/skills",
	"~/.snowflake/cortex/skills",
	"~/.tabnine/agent/skills",
	"~/.terramind/skills",
	"~/.tinycloud/skills",
	"~/.openclaw/skills",
	"~/.clawdbot/skills",
	"~/.moltbot/skills",
	"~/.zencoder/skills",
}

// envAgentHomes relocatable agent homes: when the env var is set (and
// non-blank), the loader uses that home instead of the default, so its
// skills/ directory there is what actually loads. Mirrors the env handling in
// vercel-labs/skills src/agents.ts.
var envAgentHomes = []struct{ env, fallback string }{
	{"CODEX_HOME", "~/.codex"},
	{"CLAUDE_CONFIG_DIR", "~/.claude"},
	{"VIBE_HOME", "~/.vibe"},
	{"HERMES_HOME", "~/.hermes"},
	{"AUTOHAND_HOME", "~/.autohand"},
	{"GROK_HOME", "~/.grok"},
}

// PluginCacheRoots hold plugin-provided skills. Each plugin registers its own
// skills/ directory as a load root; the depth varies, so every directory
// named skills that directly contains a SKILL.md is a candidate. Not an
// upstream concept (vercel-labs/skills only installs); discovered on real
// machines.
var PluginCacheRoots = []string{
	"~/.trae/plugins/cache",
	"~/.claude/plugins/cache",
	"~/.zcode/cli/plugins/cache",
}

// Discover finds every skill root an agent will load. Results are deduped by
// realpath. When only is non-empty, discovery is skipped and exactly those
// directories are used (tests and targeted checks).
func Discover(extra, only []string) []collect.Root {
	if len(only) > 0 {
		var out []collect.Root
		seen := map[string]bool{}
		for _, d := range only {
			p := paths.Expand(d)
			rp := paths.RealPath(p)
			if isDir(p) && !seen[rp] {
				seen[rp] = true
				out = append(out, collect.Root{Path: p, Kind: "agent"})
			}
		}
		return out
	}

	var found []collect.Root
	seen := map[string]bool{}
	add := func(path, kind string) {
		if !isDir(path) || seen[paths.RealPath(path)] || !hasSkill(path) {
			return
		}
		seen[paths.RealPath(path)] = true
		found = append(found, collect.Root{Path: path, Kind: kind})
	}

	for _, c := range AgentRootCandidates {
		add(paths.Expand(c), "agent")
	}
	// An env-set home relocates the loader's home dir; that location's
	// skills/ is what actually loads. The static candidate list already
	// covers the default, and add() dedups by realpath.
	for _, h := range envAgentHomes {
		if v := strings.TrimSpace(os.Getenv(h.env)); v != "" {
			add(filepath.Join(paths.Expand(v), "skills"), "agent")
		}
	}
	for _, c := range extra {
		add(paths.Expand(c), "agent")
	}

	// A plugin usually keeps many historical versions side by side, but the
	// agent loads only the newest one (empirically the active version = the
	// version directory with the newest mtime). Reflecting anything else
	// invents a heap of un-loadable zombie versions.
	// Layout: <cache>/<scope>/<plugin>/<version>/skills/...
	type candidate struct {
		mtime  int64
		skills string
	}
	pluginVersions := map[string][]candidate{}
	for _, cache := range PluginCacheRoots {
		base := paths.Expand(cache)
		if !isDir(base) {
			continue
		}
		_ = filepath.WalkDir(base, func(dirpath string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if dirpath != base && paths.Prune[d.Name()] {
					return filepath.SkipDir
				}
				if d.Name() == "skills" && hasDirectSkillChild(dirpath) {
					versionDir := filepath.Dir(dirpath)
					pluginDir := filepath.Dir(versionDir)
					mt := int64(0)
					if info, err := os.Stat(versionDir); err == nil {
						mt = info.ModTime().UnixNano()
					}
					pluginVersions[pluginDir] = append(pluginVersions[pluginDir],
						candidate{mtime: mt, skills: dirpath})
					return filepath.SkipDir
				}
			}
			return nil
		})
	}

	var pluginSkills []string
	for _, versions := range pluginVersions {
		newest := versions[0]
		for _, v := range versions[1:] {
			if v.mtime > newest.mtime {
				newest = v
			}
		}
		pluginSkills = append(pluginSkills, newest.skills)
	}
	// Sort for a stable root list: map iteration order is random, and the
	// state graph must compile identically twice.
	sort.Strings(pluginSkills)
	for _, p := range pluginSkills {
		add(p, "plugin")
	}
	return found
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// hasSkill reports whether the tree contains any SKILL.md (recursively,
// following symlinked directories).
func hasSkill(dirpath string) bool {
	found := false
	paths.WalkFollow(dirpath, func(_ string, names []string) bool {
		for _, n := range names {
			if n == "SKILL.md" {
				found = true
				return true
			}
		}
		return false
	})
	return found
}

// hasDirectSkillChild reports a <dir>/<name>/SKILL.md layout (one level only).
func hasDirectSkillChild(dirpath string) bool {
	entries, err := os.ReadDir(dirpath)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() && paths.Exists(filepath.Join(dirpath, e.Name(), "SKILL.md")) {
			return true
		}
	}
	return false
}
