package graph

import (
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
)

// flattenAreas walks the declared vocabulary into the flat, declaration-ordered
// list `Config.areas` is specified as: a parent immediately before the subtree
// it heads, each node carrying its depth from a root.
//
// Emitting a node BEFORE recursing into its children is the contract, not an
// incidental of the walk. The wire shape carries no `children` field, so a
// client reads a node's subtree as the maximal run of following entries with a
// greater depth; any other emission order describes a different tree while
// still type-checking.
//
// config.AreaPaths enumerates the same paths in the same order, but a caller
// here needs each node's own fields alongside its path, which a flat list of
// strings cannot give back.
//
// Values go out verbatim: config.RenderAreaPath is a terminal rendering
// boundary for CLI text, and JSON decoded into a DOM text node is a different
// one. `path` is also what a client sends back as an `area:` filter argument,
// which the server matches against the declared vocabulary exactly.
func flattenAreas(areas *config.Areas) []*model.Area {
	roots := areas.Roots()
	return appendAreaNodes(make([]*model.Area, 0, len(roots)), roots, "", 0)
}

func appendAreaNodes(out []*model.Area, areas []config.AreaConfig, parent string, depth int) []*model.Area {
	for _, area := range areas {
		path := area.Name
		if parent != "" {
			path = parent + config.AreaPathSeparator + area.Name
		}
		out = append(out, &model.Area{
			Path:        path,
			Name:        area.Name,
			Description: area.Description,
			Color:       area.Color,
			Depth:       depth,
		})
		out = appendAreaNodes(out, area.Children, path, depth+1)
	}
	return out
}
