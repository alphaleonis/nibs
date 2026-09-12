package nibcore

import (
	"sort"

	"github.com/alphaleonis/nibs/internal/nib"
)

// mentionIndex maintains a token-keyed reverse lookup so FindMentionedBy
// becomes O(matches) instead of O(N × body-length). Not safe for concurrent
// use; callers hold Core.mu.
//
// Tokens are stored as the raw text after the `#` sigil, never as resolved
// target IDs: a token that resolves to no nib at index time is still recorded,
// and InboundSources returns its source as soon as that nib is created — no
// reconciliation pass.
type mentionIndex struct {
	// outbound maps source ID -> deduped raw mention tokens in body order.
	outbound map[string][]string
	// inbound maps raw token -> set of source IDs that mention it.
	inbound map[string]map[string]struct{}
}

func newMentionIndex() *mentionIndex {
	return &mentionIndex{
		outbound: make(map[string][]string),
		inbound:  make(map[string]map[string]struct{}),
	}
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
		set, ok := m.inbound[tok]
		if !ok {
			set = make(map[string]struct{})
			m.inbound[tok] = set
		}
		set[sourceID] = struct{}{}
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
		set := m.inbound[tok]
		if set == nil {
			continue
		}
		delete(set, sourceID)
		if len(set) == 0 {
			delete(m.inbound, tok)
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
