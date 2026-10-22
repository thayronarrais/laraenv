package i18n

import "testing"

func TestMatch(t *testing.T) {
	available := []Meta{{Code: "en"}, {Code: "pt-BR"}, {Code: "ru"}}
	cases := []struct{ preferred, want string }{
		{"pt-BR", "pt-BR"},
		{"pt-PT", "pt-BR"}, // same language, the only Portuguese we ship
		{"pt", "pt-BR"},
		{"ru-RU", "ru"},
		{"en-GB", "en"},
		{"ja-JP", "en"}, // nothing close: English
		{"", "en"},
	}
	for _, tc := range cases {
		if got := Match(tc.preferred, available); got != tc.want {
			t.Fatalf("Match(%q) = %q, want %q", tc.preferred, got, tc.want)
		}
	}
}
