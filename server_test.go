package main

import "testing"

func TestRedactedMasksTokens(t *testing.T) {
	for _, tc := range []struct{ query, want string }{
		{"", ""},
		{"static=true", "static=true"},
		{"api_key=abc&static=true", "api_key=***&static=true"},
		{"static=True&ApiKey=abc&PlaySessionId=def", "static=True&ApiKey=***&PlaySessionId=***"},
		{"X-Emby-Token=abc&deviceId=kodi", "X-Emby-Token=***&deviceId=kodi"},
		{"api_key", "api_key"},
	} {
		if got := redacted(tc.query); got != tc.want {
			t.Errorf("redacted(%q) = %q, want %q", tc.query, got, tc.want)
		}
	}
}
