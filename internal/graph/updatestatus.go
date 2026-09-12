package graph

import (
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/updatecheck"
)

// updateStatusResult maps a check outcome to the GraphQL model. When the check
// had no opinion (ok=false) it reports the running version with no update.
//
// Lives here rather than in schema.resolvers.go, which would not keep it — see
// the codegen-survival note at the top of resolver.go.
func updateStatusResult(current string, res updatecheck.Result, ok bool) *model.UpdateStatus {
	if !ok {
		return &model.UpdateStatus{Current: current, Latest: "", UpdateAvailable: false}
	}
	return &model.UpdateStatus{
		Current:         res.Current,
		Latest:          res.Latest,
		UpdateAvailable: res.UpdateAvailable,
	}
}
