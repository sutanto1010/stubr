package config

import (
	"net/url"
	"testing"
)

func TestFindQueryMatch(t *testing.T) {
	dc := &DirConfig{
		QueryMatch: []QueryMatch{
			{Params: map[string]string{"itemID": "abc-123"}},
		},
	}

	// Path param match
	qm := FindQueryMatch(dc, map[string]string{"itemID": "abc-123"}, url.Values{})
	if qm == nil {
		t.Error("expected match via path param")
	}

	// Query param match
	qm = FindQueryMatch(dc, nil, url.Values{"itemID": {"abc-123"}})
	if qm == nil {
		t.Error("expected match via query param")
	}

	// No match
	qm = FindQueryMatch(dc, nil, url.Values{})
	if qm != nil {
		t.Error("expected no match")
	}

	// Path param takes precedence over mismatched query param
	qm = FindQueryMatch(dc, map[string]string{"itemID": "abc-123"}, url.Values{"itemID": {"wrong"}})
	if qm == nil {
		t.Error("expected path param to win over query param")
	}

	// Path param mismatch
	qm = FindQueryMatch(dc, map[string]string{"itemID": "wrong"}, url.Values{})
	if qm != nil {
		t.Error("expected no match when path param mismatches")
	}

	// Mixed: path param + query param
	dc2 := &DirConfig{
		QueryMatch: []QueryMatch{
			{Params: map[string]string{"batchID": "b1", "itemID": "i1"}},
		},
	}
	qm = FindQueryMatch(dc2, map[string]string{"batchID": "b1"}, url.Values{"itemID": {"i1"}})
	if qm == nil {
		t.Error("expected match with mixed path+query params")
	}
}
