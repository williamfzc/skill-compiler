// Package analysis holds derived views over nodes and edges.
//
// Three read-only computations: back-reference counts on each node, identity
// groups (same realpath mounted twice, or same content copied), and name
// collisions within one root. These annotate or summarize; they never change
// the graph's shape.
package analysis

import (
	"path/filepath"
	"sort"

	"skillscope/internal/collect"
	"skillscope/internal/edges"
)

// Collision is several skills within one root declaring the same effective
// name; on load they overwrite each other and the loader can hit only one.
type Collision struct {
	Root   string   `json:"root"`
	Name   string   `json:"name"`
	Skills []string `json:"skills"`
	Rels   []string `json:"rels"`
}

// Identities groups nodes that are physically the same thing (multi_mounted)
// or byte-identical independent copies (same_content).
type Identities struct {
	MultiMounted map[string][]string `json:"multi_mounted"`
	SameContent  map[string][]string `json:"same_content"`
}

// ComputeRefsIn annotates each node with refs_in_count, refs_out, contains,
// contained_by.
func ComputeRefsIn(nodes map[string]*collect.Node, edgelist []edges.Edge) {
	incoming := map[string]int{}
	type refPair struct{ from, to string }
	var refs []refPair
	var contains []refPair
	for _, e := range edgelist {
		switch e.Kind {
		case "ref":
			if _, ok := nodes[e.To]; ok {
				incoming[e.To]++
			}
			refs = append(refs, refPair{e.From, e.To})
		case "contains":
			contains = append(contains, refPair{e.From, e.To})
		}
	}
	for nid, node := range nodes {
		node.RefsInCount = incoming[nid]
		out := map[string]bool{}
		cont := map[string]bool{}
		contBy := map[string]bool{}
		for _, p := range refs {
			if p.from == nid {
				out[p.to] = true
			}
		}
		for _, p := range contains {
			if p.from == nid {
				cont[p.to] = true
			}
			if p.to == nid {
				contBy[p.from] = true
			}
		}
		node.RefsOut = sortedKeys(out)
		node.Contains = sortedKeys(cont)
		node.ContainedBy = sortedKeys(contBy)
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// GroupIdentities finds multi-mounted nodes (one realpath reached via several
// roots/symlinks) and same-content groups (different realpaths but identical
// SKILL.md content; editing one leaves the others out of sync).
func GroupIdentities(nodes map[string]*collect.Node) Identities {
	multiMounted := map[string][]string{}
	for nid, node := range nodes {
		if len(node.Provenance) > 1 {
			var mounts []string
			for _, p := range node.Provenance {
				mounts = append(mounts, filepath.Join(p.Root, p.Rel))
			}
			multiMounted[nid] = mounts
		}
	}

	type key struct{ eff, hash string }
	byHash := map[key][]string{}
	for nid, node := range nodes {
		if node.ContentHash == "" {
			continue
		}
		eff := node.NameField
		if eff == "" {
			eff = node.Name
		}
		k := key{eff, node.ContentHash}
		byHash[k] = append(byHash[k], nid)
	}
	sameContent := map[string][]string{}
	for k, ids := range byHash {
		if len(ids) > 1 {
			sorted := append([]string(nil), ids...)
			sort.Strings(sorted)
			sameContent[k.eff+"@"+k.hash] = sorted
		}
	}
	return Identities{MultiMounted: multiMounted, SameContent: sameContent}
}

// DetectNameCollisions finds same effective name within one root. Same name
// across different roots is not a collision (each root is its own namespace,
// and plugins even add a prefix); those surface as identities instead.
func DetectNameCollisions(nodes map[string]*collect.Node) []Collision {
	type nsKey struct{ root, name string }
	byNS := map[nsKey]map[string]bool{}
	for nid, node := range nodes {
		if node.BrokenSymlink {
			continue
		}
		eff := node.NameField
		if eff == "" {
			eff = node.Name
		}
		for _, prov := range node.Provenance {
			k := nsKey{prov.Root, eff}
			if byNS[k] == nil {
				byNS[k] = map[string]bool{}
			}
			byNS[k][nid] = true
		}
	}
	keys := make([]nsKey, 0, len(byNS))
	for k := range byNS {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].root != keys[j].root {
			return keys[i].root < keys[j].root
		}
		return keys[i].name < keys[j].name
	})
	var out []Collision
	for _, k := range keys {
		ids := byNS[k]
		if len(ids) <= 1 {
			continue
		}
		idList := make([]string, 0, len(ids))
		for id := range ids {
			idList = append(idList, id)
		}
		sort.Strings(idList)
		var rels []string
		for _, id := range idList {
			for _, p := range nodes[id].Provenance {
				if p.Root == k.root {
					rels = append(rels, p.Rel)
				}
			}
		}
		sort.Strings(rels)
		out = append(out, Collision{
			Root: k.root, Name: k.name, Skills: idList, Rels: rels,
		})
	}
	return out
}
