package main

import "testing"

func TestNormalizeRefs(t *testing.T) {
	t.Parallel()

	if got := normalizeSpaceName("AAA123"); got != "spaces/AAA123" {
		t.Fatalf("normalizeSpaceName added prefix incorrectly: got %q", got)
	}
	if got := normalizeSpaceName("spaces/AAA123"); got != "spaces/AAA123" {
		t.Fatalf("normalizeSpaceName changed existing prefix: got %q", got)
	}
	if got := normalizeUserRef("alice@example.com"); got != "users/alice@example.com" {
		t.Fatalf("normalizeUserRef added prefix incorrectly: got %q", got)
	}
	if got := normalizeUserRef("users/123"); got != "users/123" {
		t.Fatalf("normalizeUserRef changed existing prefix: got %q", got)
	}
}

func TestParseMessageTime(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		input  string
		expect bool
	}{
		{name: "RFC3339Nano", input: "2026-02-17T12:34:56.123456Z", expect: true},
		{name: "RFC3339", input: "2026-02-17T12:34:56Z", expect: true},
		{name: "empty", input: "", expect: false},
		{name: "invalid", input: "not-a-time", expect: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, ok := parseMessageTime(tc.input)
			if ok != tc.expect {
				t.Fatalf("parseMessageTime(%q) ok=%v, expected %v", tc.input, ok, tc.expect)
			}
		})
	}
}

func TestPersonMatchScoreOrdering(t *testing.T) {
	t.Parallel()

	exactDisplay := personMatchScore("simon", "Simon", "users/simon@example.com")
	prefixDisplay := personMatchScore("sim", "Simon", "users/simon@example.com")
	containsDisplay := personMatchScore("imo", "Simon", "users/simon@example.com")
	userOnly := personMatchScore("simon@example", "", "users/simon@example.com")
	none := personMatchScore("zzz", "Simon", "users/simon@example.com")

	if !(exactDisplay > prefixDisplay && prefixDisplay > containsDisplay) {
		t.Fatalf("expected display score order exact > prefix > contains, got %d, %d, %d", exactDisplay, prefixDisplay, containsDisplay)
	}
	if userOnly <= 0 {
		t.Fatalf("expected positive score for user-only match, got %d", userOnly)
	}
	if none != 0 {
		t.Fatalf("expected zero score for no match, got %d", none)
	}
}

func TestRunChatMessagesRecentValidation(t *testing.T) {
	t.Parallel()

	if err := runChatMessagesRecent([]string{}); err == nil || err.Error() != "one of --email or --user or --name is required" {
		t.Fatalf("unexpected error for missing identity: %v", err)
	}

	if err := runChatMessagesRecent([]string{"--name", "Simon", "--email", "simon@example.com"}); err == nil || err.Error() != "use exactly one of --email, --user, or --name" {
		t.Fatalf("unexpected error for multiple identities: %v", err)
	}

	if err := runChatMessagesRecent([]string{"--name", "Simon", "--limit", "0"}); err == nil || err.Error() != "--limit must be greater than 0" {
		t.Fatalf("unexpected error for invalid limit: %v", err)
	}
}

