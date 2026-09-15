package graph

import (
	"github.com/alphaleonis/nibs/internal/area"
	"github.com/alphaleonis/nibs/internal/graph/model"
)

func configResult(reader NibReader) *model.Config {
	return configResultWithAreas(reader, reader.Areas())
}

// configResultWithAreas is configResult over a vocabulary the caller already
// holds. An area mutation answers with the vocabulary its own edit wrote and
// re-read under the store's write lock; re-asking the reader would assume the
// reader and the area writer are backed by the same store.
func configResultWithAreas(reader NibReader, areas *area.Vocabulary) *model.Config {
	cfg := reader.Config()
	return &model.Config{
		ProjectName: cfg.GetProjectName(),
		Prefix:      cfg.Nibs.Prefix,
		Areas:       flattenAreas(areas),
	}
}

// flattenAreas walks the declared vocabulary into the flat list `Config.areas`
// is specified as: siblings by name (area.Vocabulary.Roots), a parent immediately
// before the subtree it heads, each node carrying its depth from a root.
//
// Emit a node BEFORE recursing into its children. The wire shape carries no
// `children` field, so a client reads a node's subtree as the maximal run of
// following entries with a greater depth.
//
// Values go out verbatim. area.RenderPath neutralizes a path for a
// MESSAGE; a `path` here is data — what a client sends back as an `area:` filter
// argument, matched against the declared vocabulary exactly.
func flattenAreas(areas *area.Vocabulary) []*model.Area {
	roots := areas.Roots()
	return appendAreaNodes(make([]*model.Area, 0, len(roots)), roots, "", 0)
}

func appendAreaNodes(out []*model.Area, areas []area.Node, parent string, depth int) []*model.Area {
	for _, node := range areas {
		path := area.JoinPath(parent, node.Name)
		out = append(out, &model.Area{
			Path:        path,
			Name:        node.Name,
			Description: node.Description,
			Color:       node.Color,
			Depth:       depth,
		})
		out = appendAreaNodes(out, node.Children, path, depth+1)
	}
	return out
}
