package viewshape

import (
	"fmt"
	"slices"

	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/membership"
	"github.com/alphaleonis/nibs/internal/nib"
)

// The keys of the leftover sections, as web/src/lib/viewSpine.ts names them.
const (
	BacklogSection = "/__backlog__"
	NoAreaSection  = "/__no_area__"
)

// placementKind says how a lens places one nib.
type placementKind int

const (
	// placeMember puts the nib in a section by something other than heading it.
	placeMember placementKind = iota
	// placeHeader makes the nib's own row the section's row.
	placeHeader
	// placeHidden gives the nib no row. Under a lens that nests structurally,
	// its children are placed in its stead.
	placeHidden
)

type placement struct {
	kind    placementKind
	section string
}

// declaredSection is a section a lens shows whether or not anything lands in it.
type declaredSection struct {
	key      string
	children []declaredSection
}

// shapeInput is what a lens may read while it places nibs.
type shapeInput struct {
	membership *membership.View
	areas      *area.Vocabulary
}

// lens is how a grouped level arranges nibs into sections, decided per nib. A
// grouped level is one lens value in lenses.
type lens struct {
	// leftover is the key of the section for the nibs no other section takes.
	leftover string
	// declared returns the sections shown even when empty, in order, or nil.
	// Shape refuses a nib heading a declared section that declares sections
	// inside it.
	declared func(in *shapeInput) []declaredSection
	// place answers the same way for the same nib and input.
	place func(b *nib.Nib, in *shapeInput) placement
	// nestStructurally makes a section header or member bring its whole subtree
	// along. Without it, every nib is placed on its own, and each section nests
	// only the nibs that landed in it.
	nestStructurally bool
	// memberGroup returns the ordering group of the rows directly inside a
	// section, or the zero Group to leave each row in its parent group.
	memberGroup func(section string) Group
	// queueKey returns the order key of the rows directly inside a section, or
	// nil to keep them in `order` key order.
	queueKey func(section string) func(*nib.Nib) string
}

var lenses = map[Level]*lens{
	LevelMilestones: {
		leftover: BacklogSection,
		place: func(b *nib.Nib, in *shapeInput) placement {
			if b.EffectiveType() == "milestone" {
				return placement{kind: placeHeader, section: b.ID}
			}
			if ms := in.membership.MilestoneOf(b.ID); ms != "" {
				return placement{kind: placeMember, section: ms}
			}
			return placement{kind: placeMember, section: BacklogSection}
		},
		memberGroup: func(section string) Group {
			if section == BacklogSection {
				return Group{}
			}
			return Group{Axis: GroupMilestone, ID: section}
		},
		queueKey: func(section string) func(*nib.Nib) string {
			if section == BacklogSection {
				return nil
			}
			return func(b *nib.Nib) string { return b.MilestoneOrder }
		},
	},
	LevelAreas: {
		leftover: NoAreaSection,
		declared: func(in *shapeInput) []declaredSection {
			return declareAreas(in.areas.Roots(), "")
		},
		// An undeclared `area:` falls into the leftover, so every section key is a
		// declared path.
		place: func(b *nib.Nib, in *shapeInput) placement {
			if in.areas.Exists(b.Area) {
				return placement{kind: placeMember, section: b.Area}
			}
			return placement{kind: placeMember, section: NoAreaSection}
		},
		// The server keeps no order key per area.
		memberGroup: func(string) Group { return Group{} },
		queueKey:    func(string) func(*nib.Nib) string { return nil },
	},
}

func declareAreas(nodes []area.Node, parent string) []declaredSection {
	sections := make([]declaredSection, 0, len(nodes))
	for _, node := range nodes {
		path := area.JoinPath(parent, node.Name)
		sections = append(sections, declaredSection{key: path, children: declareAreas(node.Children, path)})
	}
	return sections
}

// IsLeftoverSection reports whether key names the section a grouped level puts
// the nibs in that no other section takes.
func IsLeftoverSection(key string) bool {
	for _, l := range lenses {
		if l.leftover == key {
			return true
		}
	}
	return false
}

// section is one section of a grouped view while it is assembled.
type section struct {
	key      string
	declared bool
	header   *node
	members  []*node
	// subsections are the sections declared inside this one, in order.
	subsections []*section
}

// shapeGrouped arranges ordered into l's sections. The sections come in this
// order: declared ones as declared, then those a nib heads, then any others,
// then the leftover; each group in the order its first nib reached it.
func shapeGrouped(ordered []*nib.Nib, l *lens, in *shapeInput) ([]*node, error) {
	placements := make(map[string]placement, len(ordered))
	for _, b := range ordered {
		placements[b.ID] = l.place(b, in)
	}

	sections := make(map[string]*section)
	var reached []*section
	sectionFor := func(key string) *section {
		s, ok := sections[key]
		if !ok {
			s = &section{key: key}
			sections[key] = s
			reached = append(reached, s)
		}
		return s
	}

	var declare func(decls []declaredSection) ([]*section, error)
	declare = func(decls []declaredSection) ([]*section, error) {
		var out []*section
		for _, d := range decls {
			if d.key == l.leftover {
				return nil, fmt.Errorf("viewshape: declared section %q is the leftover's key", d.key)
			}
			if _, dup := sections[d.key]; dup {
				return nil, fmt.Errorf("viewshape: section %q is declared twice", d.key)
			}
			s := sectionFor(d.key)
			s.declared = true
			subsections, err := declare(d.children)
			if err != nil {
				return nil, err
			}
			s.subsections = subsections
			out = append(out, s)
		}
		return out, nil
	}
	var declaredRoots []*section
	if l.declared != nil {
		var err error
		if declaredRoots, err = declare(l.declared(in)); err != nil {
			return nil, err
		}
	}

	if l.nestStructurally {
		var walk func(nodes []*node)
		walk = func(nodes []*node) {
			for _, n := range nodes {
				p := placements[n.nib.ID]
				if p.kind == placeHidden {
					walk(n.children)
					continue
				}
				s := sectionFor(p.section)
				// A second nib heading a section joins it as a member.
				if p.kind == placeHeader && s.header == nil {
					s.header = n
				} else {
					s.members = append(s.members, n)
				}
			}
		}
		walk(buildForest(ordered))
	} else {
		// Headers first, so the sections nibs head come in the order of their
		// headers.
		for _, b := range ordered {
			if p := placements[b.ID]; p.kind == placeHeader {
				if s := sectionFor(p.section); s.header == nil {
					s.header = &node{nib: b}
				}
			}
		}
		var keys []string
		memberNibs := make(map[string][]*nib.Nib)
		for _, b := range ordered {
			p := placements[b.ID]
			if p.kind == placeHidden {
				continue
			}
			if s := sectionFor(p.section); s.header != nil && s.header.nib == b {
				continue
			}
			if _, ok := memberNibs[p.section]; !ok {
				keys = append(keys, p.section)
			}
			memberNibs[p.section] = append(memberNibs[p.section], b)
		}
		for _, key := range keys {
			sections[key].members = buildForest(memberNibs[key])
		}
	}

	var headed, headless []*section
	var leftover *section
	for _, s := range reached {
		switch {
		case s.key == l.leftover:
			leftover = s
		case s.declared:
			// The header's own children would share the section with the declared
			// sections.
			if s.header != nil && len(s.subsections) > 0 {
				return nil, fmt.Errorf("viewshape: nib %q heads section %q, which declares sections inside it", s.header.nib.ID, s.key)
			}
		case s.header != nil:
			headed = append(headed, s)
		default:
			headless = append(headless, s)
		}
	}
	emitted := slices.Concat(declaredRoots, headed, headless)
	if leftover != nil {
		emitted = append(emitted, leftover)
	}
	roots := make([]*node, 0, len(emitted))
	for _, s := range emitted {
		roots = append(roots, assemble(s, l))
	}
	return roots, nil
}

// assemble returns the node that draws s.
func assemble(s *section, l *lens) *node {
	members := s.members
	if key := l.queueKey(s.key); key != nil {
		members = slices.Clone(members)
		slices.SortStableFunc(members, func(a, b *node) int { return nib.CompareByKey(a.nib, b.nib, key) })
	}
	group := l.memberGroup(s.key)
	if s.header != nil {
		return &node{
			nib:         s.header.nib,
			heads:       s.key,
			memberGroup: group,
			children:    slices.Concat(s.header.children, members),
		}
	}
	children := make([]*node, 0, len(s.subsections)+len(members))
	for _, sub := range s.subsections {
		children = append(children, assemble(sub, l))
	}
	return &node{
		heads:       s.key,
		persists:    s.declared,
		memberGroup: group,
		children:    append(children, members...),
	}
}
