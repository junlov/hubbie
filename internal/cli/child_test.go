package cli

import "testing"

func TestWhoamiPortal(t *testing.T) {
	tests := map[string]string{
		"User:    a@example.com\nPortal:  1000001\n": "1000001",
		"  Portal: 7 (sandbox)\n":                    "7",
		`{"portal_id":123}`:                          "123",
		`{"portal":"456"}`:                           "456",
		`{"hub_id":789}`:                             "789",
		`{"data":{"portal_id":11}}`:                  "11",
		`{"data":{"portal":"12"}}`:                   "12",
		`{"portal_id":null,"portal":13}`:             "13",
		`{"portal":"abc"}`:                           "",
		`{"unrelated":1}`:                            "",
		"no portal here":                             "",
	}
	for in, want := range tests {
		if got := whoamiPortal([]byte(in)); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestWithEnv(t *testing.T) {
	got := withEnv([]string{"A=1", "HOME=/old", "B=2"}, "HOME=/new")
	want := []string{"A=1", "B=2", "HOME=/new"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("got %v", got)
	}
}
