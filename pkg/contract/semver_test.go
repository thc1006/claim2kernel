package contract

import "testing"

func TestVersionRanges(t *testing.T) {
	cases := []struct {
		v, r string
		want bool
	}{
		{"1.0.0b2", ">=1.0.0b2,<1.1.0", true},
		{"1.0.0b1", ">=1.0.0b2,<1.1.0", false},
		{"1.0.0b10", ">1.0.0b2,<1.1.0", true},
		{"1.0.0rc1", ">1.0.0b10,<1.0.0", true},
		{"1.0.0a9", ">=1.0.0b2", false},
		{"1.0.0", ">=1.0.0b2,<1.1.0", true},
		{"1.1.0", ">=1.0.0b2,<1.1.0", false},
		{"1.36.3", ">=1.36.0,<1.37.0", true},
	}
	for _, tc := range cases {
		got, err := MatchVersionRange(tc.v, tc.r)
		if err != nil {
			t.Fatalf("%s in %s: %v", tc.v, tc.r, err)
		}
		if got != tc.want {
			t.Fatalf("%s in %s: got %v want %v", tc.v, tc.r, got, tc.want)
		}
	}
}
