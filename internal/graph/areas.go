package graph

import (
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
)

// configResult builds the wire Config from the reader as it stands NOW. Every
// surface that answers with one goes through it — the `config` query, the
// `configChanged` subscription, and the two area mutations — so a field added to
// Config reaches all four at once rather than three of them.
func configResult(reader NibReader) *model.Config {
	return configResultWithAreas(reader, reader.Areas())
}

// configResultWithAreas is configResult over a vocabulary the caller already
// holds. The area mutations answer through it with the vocabulary their own edit
// wrote and re-read under the store's write lock, rather than re-asking the
// reader — which would make the answer depend on Reader and the area writer
// being backed by the same store, a requirement nothing could enforce and whose
// violation is a silent wrong answer rather than a compile error.
func configResultWithAreas(reader NibReader, areas *config.Areas) *model.Config {
	cfg := reader.Config()
	return &model.Config{
		ProjectName: cfg.GetProjectName(),
		Prefix:      cfg.Nibs.Prefix,
		Areas:       flattenAreas(areas),
	}
}

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
