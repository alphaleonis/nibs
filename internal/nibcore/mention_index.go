package nibcore

import (
	"sort"

	"github.com/alphaleonis/nibs/internal/nib"
)

// mentionIndex maintains a token-keyed reverse lookup so FindMentionedBy
// becomes O(matches) instead of O(N × body-length). Not safe for concurrent
// use; callers hold Core.mu.
//
// The index stores tokens as the raw text after the `#` sigil, not as
// resolved target IDs. That keeps late-bound targets working naturally:
// a source may mention a token that does not resolve to any nib at index
// time; when such a nib is later created, InboundSources for its ID (or
// short form) simply starts returning the source — no reconciliation pass
// is needed.
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

// Add parses body via nib.ExtractMentionTokens and records the source's
// mention tokens. If a prior record exists for the source, it is cleared
// first — callers that only want the "additive" semantics should check
// outbound membership themselves before calling.
func (m *mentionIndex) Add(sourceID, body string) {
	// Defensive: if a prior entry existed, drop it first so inbound stays
	// consistent.
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

// Replace is Remove followed by Add. Callers don't need to track whether
// the source already existed.
func (m *mentionIndex) Replace(sourceID, body string) {
	m.Remove(sourceID)
	m.Add(sourceID, body)
}

// Rebuild clears the index and re-populates it from the given nib map.
// Reads only the Body field; no Core dependency.
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
// freely without corrupting the index. Symmetric with InboundSources.
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
