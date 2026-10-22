package agents

import "testing"

func TestPathWithin(t *testing.T) {
	cases := []struct {
		root, candidate string
		want            bool
	}{
		{`C:\laraenv\www\demo`, `C:\laraenv\www\demo`, true},
		{`C:\laraenv\www\demo`, `c:/laraenv/www/DEMO/app/Http`, true},
		{`C:\laraenv\www\demo`, `C:\laraenv\www\demo-api`, false},
		{`C:\laraenv\www\demo`, `C:\laraenv\www\demo\..\other`, false},
		{`C:\laraenv\www\demo`, `C:\laraenv\www`, false},
		{`C:\laraenv\www\demo`, ``, false},
		{``, `C:\laraenv\www\demo`, false},
	}
	for _, tc := range cases {
		if got := PathWithin(tc.root, tc.candidate); got != tc.want {
			t.Fatalf("PathWithin(%q, %q) = %v, want %v", tc.root, tc.candidate, got, tc.want)
		}
	}
}
