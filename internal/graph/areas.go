package graph

import (
	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
)

func configResult(reader NibReader) *model.Config {
	return configResultWithAreas(reader, reader.Areas())
}

// configResultWithAreas is configResult over a vocabulary the caller already
// holds. An area mutation answers with the vocabulary its own edit wrote and
// re-read under the store's write lock; re-asking the reader would assume the
// reader and the area writer are backed by the same store.
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
// Emit a node BEFORE recursing into its children. The wire shape carries no
// `children` field, so a client reads a node's subtree as the maximal run of
// following entries with a greater depth.
//
// Values go out verbatim. config.RenderAreaPath neutralizes a path for a
// MESSAGE; a `path` here is data — what a client sends back as an `area:` filter
// argument, matched against the declared vocabulary exactly.
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
