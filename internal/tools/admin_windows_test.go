package tools

import "testing"

func TestEventQuery(t *testing.T) {
	q, err := eventQuery(LogQueryInput{Priority: "error", Unit: "O'Brien", EventID: 7036})
	if err != nil {
		t.Fatal(err)
	}
	if want := "*[System[(Level=1 or Level=2) and Provider[@Name='OBrien'] and EventID=7036]]"; q != want {
		t.Errorf("query = %s, want %s", q, want)
	}
	if q, _ := eventQuery(LogQueryInput{}); q != "*" {
		t.Errorf("empty query = %s", q)
	}
	if _, err := eventQuery(LogQueryInput{Priority: "loud"}); err == nil {
		t.Error("unknown priority should be rejected")
	}
}
