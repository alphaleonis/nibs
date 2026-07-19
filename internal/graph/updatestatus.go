package graph

import (
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/updatecheck"
)

// updateStatusResult maps a check outcome to the GraphQL model. When the check
// had no opinion (ok=false) it reports the running version with no update, so
// the updateStatus query never fails and never claims a false positive.
//
// Lives here rather than in the generated schema.resolvers.go: gqlgen comments
// out free helper functions it finds in resolver files on regeneration.
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
