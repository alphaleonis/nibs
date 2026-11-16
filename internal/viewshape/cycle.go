package viewshape

import "github.com/alphaleonis/nibs/internal/nib"

// PromotedCycleRoots picks the lowest-id member of every parent cycle lying
// wholly inside nibs. A tree builder promotes each to a root, so the rest of its
// cycle nests beneath it.
//
// web/src/lib/tree.ts applies the same rule; keep the two in agreement. Go
// compares bytes and TypeScript UTF-16 code units, so ids holding
// supplementary-plane characters can promote different members.
//
// Each nib is walked once: unseen -> onPath -> settled.
func PromotedCycleRoots(nibs map[string]*nib.Nib) map[string]bool {
	const (
		unseen = iota
		onPath
		settled
	)
	state := make(map[string]int, len(nibs))
	promoted := make(map[string]bool)

	for id := range nibs {
		if state[id] != unseen {
			continue
		}
		// Follow the parent chain until it leaves the set, ends, or revisits
		// the path.
		var path []string
		for cur := id; ; {
			if state[cur] == onPath {
				// The cycle is the path from cur onward; entries before it lead
				// into the cycle.
				start := 0
				for i, m := range path {
					if m == cur {
						start = i
						break
					}
				}
				lowest := path[start]
				for _, m := range path[start+1:] {
					if m < lowest {
						lowest = m
					}
				}
				promoted[lowest] = true
				break
			}
			if state[cur] == settled {
				// Explored already, along with any cycle beyond it.
				break
			}
			state[cur] = onPath
			path = append(path, cur)
			b := nibs[cur]
			if b.Parent == "" {
				break
			}
			if _, ok := nibs[b.Parent]; !ok {
				break
			}
			cur = b.Parent
		}
		for _, m := range path {
			state[m] = settled
		}
	}

	return promoted
}
