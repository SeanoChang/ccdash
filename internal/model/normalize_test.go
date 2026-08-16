package model

import "testing"

func TestNormalizeModel(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"claude-haiku-4-5-20251001", "claude-haiku-4-5"},
		{"claude-haiku-4-5", "claude-haiku-4-5"},
		{"claude-opus-5", "claude-opus-5"},
		{"gpt-5.6-luna", "gpt-5.6-luna"},
		{"gpt-5-codex", "gpt-5-codex"},
		{"<synthetic>", "<synthetic>"},
		{"", ""},
		{"claude-4-5", "claude-4-5"},
	}
	for _, tc := range cases {
		if got := NormalizeModel(tc.in); got != tc.want {
			t.Errorf("NormalizeModel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
