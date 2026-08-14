package extract

import "testing"

// Captured verbatim from a real problem link on /selections/2025-summer-common.
const observedFilters = "%257B%2522difficulty%2522%253A%255B%255D%252C%2522search%2522%253A%2522%2522%252C" +
	"%2522sort%2522%253Anull%252C%2522status%2522%253A%255B%255D%252C%2522currentPage%2522%253A1%252C" +
	"%2522pageSize%2522%253A20%252C%2522tag%2522%253A%255B%255D%252C%2522language%2522%253A%255B%255D%252C" +
	"%2522groups%2522%253A%255B%255D%257D"

func TestEncodeFiltersMatchesObserved(t *testing.T) {
	got, err := EncodeFilters(DefaultFilters(1))
	if err != nil {
		t.Fatal(err)
	}
	if got != observedFilters {
		t.Errorf("encoding drifted from the observed format\ngot  %s\nwant %s", got, observedFilters)
	}
}

func TestDecodeObservedFilters(t *testing.T) {
	f, err := DecodeFilters(observedFilters)
	if err != nil {
		t.Fatal(err)
	}
	if f.CurrentPage != 1 {
		t.Errorf("CurrentPage = %d, want 1", f.CurrentPage)
	}
	if f.PageSize != 20 {
		t.Errorf("PageSize = %d, want 20", f.PageSize)
	}
}

func TestFiltersRoundTrip(t *testing.T) {
	in := DefaultFilters(3)
	enc, err := EncodeFilters(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecodeFilters(enc)
	if err != nil {
		t.Fatal(err)
	}
	if out.CurrentPage != 3 || out.PageSize != 20 {
		t.Errorf("round trip lost data: %+v", out)
	}
}

func TestDecodeFiltersRejectsGarbage(t *testing.T) {
	if _, err := DecodeFilters("not-encoded-json"); err == nil {
		t.Fatal("expected an error decoding garbage")
	}
}
