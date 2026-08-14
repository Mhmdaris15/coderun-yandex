package extract

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// Filters mirrors the JSON blob CodeRun stores in the ?filters= query
// parameter. Field order is load-bearing: it must match the site's own
// serialisation byte for byte, and encoding/json emits fields in declaration
// order.
//
// This is an undocumented UI internal and a known fragile point. If problem
// listing ever starts returning page 1 repeatedly, suspect this struct first.
type Filters struct {
	Difficulty  []string `json:"difficulty"`
	Search      string   `json:"search"`
	Sort        *string  `json:"sort"`
	Status      []string `json:"status"`
	CurrentPage int      `json:"currentPage"`
	PageSize    int      `json:"pageSize"`
	Tag         []string `json:"tag"`
	Language    []string `json:"language"`
	Groups      []string `json:"groups"`
}

// DefaultFilters returns the filter set the site itself uses for a given page.
// PageSize stays at the observed 20; raising an unvalidated parameter against
// an undocumented endpoint is exactly the sort of thing that gets an account
// flagged.
func DefaultFilters(page int) Filters {
	return Filters{
		Difficulty:  []string{},
		Search:      "",
		Sort:        nil,
		Status:      []string{},
		CurrentPage: page,
		PageSize:    20,
		Tag:         []string{},
		Language:    []string{},
		Groups:      []string{},
	}
}

// EncodeFilters produces the value that follows "filters=" in a URL. The site
// escapes the JSON twice, so "{" arrives as "%257B" rather than "%7B".
func EncodeFilters(f Filters) (string, error) {
	raw, err := json.Marshal(f)
	if err != nil {
		return "", fmt.Errorf("marshal filters: %w", err)
	}
	return url.QueryEscape(url.QueryEscape(string(raw))), nil
}

func DecodeFilters(v string) (Filters, error) {
	var f Filters

	once, err := url.QueryUnescape(v)
	if err != nil {
		return f, fmt.Errorf("filters: first unescape: %w", err)
	}
	twice, err := url.QueryUnescape(once)
	if err != nil {
		return f, fmt.Errorf("filters: second unescape: %w", err)
	}
	if err := json.Unmarshal([]byte(twice), &f); err != nil {
		return f, fmt.Errorf("filters: unmarshal %q: %w", twice, err)
	}
	return f, nil
}
