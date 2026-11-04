package nibcore

import (
	"maps"
	"sort"

	"github.com/alphaleonis/nibs/internal/nib"
)

// mentionIndex maintains a token-keyed reverse lookup so FindMentionedBy
// becomes O(matches) instead of O(N × body-length). Not safe for concurrent
// use: Core's working index is written under Core.mu, and an index returned by
// snapshot is only read.
//
// Tokens are stored as the raw id text of the mention, never as resolved
// target IDs: a token that resolves to no nib at index time is still recorded,
// and InboundSources returns its source as soon as that nib is created — no
// reconciliation pass.
type mentionIndex struct {
	// outbound maps source ID -> deduped raw mention tokens in body order.
	outbound map[string][]string
	// inbound maps raw token -> set of source IDs that mention it.
	inbound map[string]map[string]struct{}
	// owned names the inbound sets this index may write in place. Every other set
	// is shared with an index snapshot returned, so a write clones it first. An
	// outbound slice is never written in place, so it needs no such guard.
	owned map[string]struct{}
}

func newMentionIndex() *mentionIndex {
	return &mentionIndex{
		outbound: make(map[string][]string),
		inbound:  make(map[string]map[string]struct{}),
		owned:    make(map[string]struct{}),
	}
}

// snapshot returns a read-only copy of the index, at the cost of copying the two
// outer maps. The copy shares every inbound set with m, so m clones a set before
// its next write to it.
func (m *mentionIndex) snapshot() *mentionIndex {
	snap := &mentionIndex{
		outbound: maps.Clone(m.outbound),
		inbound:  maps.Clone(m.inbound),
	}
	m.owned = make(map[string]struct{})
	return snap
}

// writableInbound returns the inbound set for token that m may write in place,
// creating it when absent.
func (m *mentionIndex) writableInbound(token string) map[string]struct{} {
	set := m.inbound[token]
	if _, ok := m.owned[token]; ok && set != nil {
		return set
	}
	set = maps.Clone(set)
	if set == nil {
		set = make(map[string]struct{})
	}
	m.inbound[token] = set
	m.owned[token] = struct{}{}
	return set
}

// Add records the mention tokens in body for sourceID, replacing any record it
// already had.
func (m *mentionIndex) Add(sourceID, body string) {
	if _, exists := m.outbound[sourceID]; exists {
		m.Remove(sourceID)
	}
	tokens := nib.ExtractMentionTokens(body)
	if len(tokens) == 0 {
		return
	}
	// ExtractMentionTokens already dedupes and preserves first-appearance order.
	m.outbound[sourceID] = tokens
	for _, tok := range tokens {
		m.writableInbound(tok)[sourceID] = struct{}{}
	}
}

// Remove drops sourceID from outbound and from every inbound set it
// participated in.
func (m *mentionIndex) Remove(sourceID string) {
	tokens, ok := m.outbound[sourceID]
	if !ok {
		return
	}
	delete(m.outbound, sourceID)
	for _, tok := range tokens {
		if m.inbound[tok] == nil {
			continue
		}
		set := m.writableInbound(tok)
		delete(set, sourceID)
		if len(set) == 0 {
			delete(m.inbound, tok)
			delete(m.owned, tok)
		}
	}
}

// Replace is Remove followed by Add.
func (m *mentionIndex) Replace(sourceID, body string) {
	m.Remove(sourceID)
	m.Add(sourceID, body)
}

// Rebuild clears the index and re-populates it from the given nib map.
func (m *mentionIndex) Rebuild(nibs map[string]*nib.Nib) {
	m.outbound = make(map[string][]string, len(nibs))
	m.inbound = make(map[string]map[string]struct{})
	m.owned = make(map[string]struct{})
	for id, b := range nibs {
		if b == nil || b.Body == "" {
			continue
		}
		m.Add(id, b.Body)
	}
}

// OutboundTokens returns the deduped mention tokens recorded for sourceID
// in body order. Returns nil when the source has no mentions (or is unknown).
// The returned slice is a fresh copy — callers may retain or modify it
// freely without corrupting the index.
func (m *mentionIndex) OutboundTokens(sourceID string) []string {
	return append([]string(nil), m.outbound[sourceID]...)
}

// InboundSources returns the source IDs that mention token, sorted by ID
// ascending for deterministic iteration. Returns nil when no source
// mentions the token.
func (m *mentionIndex) InboundSources(token string) []string {
	set := m.inbound[token]
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
