package main

import (
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/profile"
)

func TestDeriveSlugFromDisplayName(t *testing.T) {
	cases := []struct {
		display string
		sender  string
		want    string
	}{
		{"Alice", "discord:214611", "alice"},
		{"Alice Doe", "discord:214611", "alice-doe"},
		{"@al!ce", "discord:214611", "al-ce"},
		{"   ", "discord:214611", "214611"},
		{"", "discord:214611", "214611"},
		{"", "raw-id", "raw-id"},
		{"", "", "user"},
		{"!!!", "x:y", "y"},
	}
	for _, c := range cases {
		got := deriveSlug(c.display, c.sender)
		if got != c.want {
			t.Errorf("deriveSlug(%q, %q) = %q, want %q", c.display, c.sender, got, c.want)
		}
	}
}

func TestBuildCoreUsersProfileOnly(t *testing.T) {
	store := profile.NewStore(t.TempDir())
	if err := store.SetAnswerTheme("discord:1", "Alice", profile.ThemeBrief); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAnswerTheme("discord:2", "Bob", profile.ThemeClear); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	out := buildCoreUsers(store, cfg)
	if len(out) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(out), out)
	}
	// Sorted by display name
	if out[0].DisplayName != "Alice" || out[1].DisplayName != "Bob" {
		t.Errorf("expected Alice then Bob, got %q then %q", out[0].DisplayName, out[1].DisplayName)
	}
	if out[0].Slug != "alice" {
		t.Errorf("Alice slug = %q, want alice", out[0].Slug)
	}
	if out[0].AnswerTheme != profile.ThemeBrief {
		t.Errorf("Alice theme = %q, want brief", out[0].AnswerTheme)
	}
	if len(out[0].Sources) != 1 || out[0].Sources[0] != "profile" {
		t.Errorf("Alice sources = %v, want [profile]", out[0].Sources)
	}
}

func TestBuildCoreUsersUnionsRoutingAndProfile(t *testing.T) {
	store := profile.NewStore(t.TempDir())
	_ = store.SetAnswerTheme("discord:1", "Alice", profile.ThemeClear)
	cfg := &config.Config{
		Routing: config.RoutingConfig{
			Mappings: []config.RoutingMapping{
				{Channel: "#als-rct", AllowedSenders: config.FlexibleStringSlice{"discord:1", "discord:2"}},
				{Channel: "#shared", AllowedSenders: config.FlexibleStringSlice{"*", "discord:3"}},
			},
		},
	}
	out := buildCoreUsers(store, cfg)
	if len(out) != 3 {
		t.Fatalf("expected 3 unique users, got %d: %+v", len(out), out)
	}
	// "*" wildcard must NOT become a user.
	for _, u := range out {
		if u.SenderID == "*" {
			t.Error("wildcard `*` leaked into user list")
		}
	}
	// discord:1 should have BOTH profile and routing source.
	for _, u := range out {
		if u.SenderID == "discord:1" {
			if len(u.Sources) != 2 {
				t.Errorf("discord:1 sources = %v, want 2", u.Sources)
			}
		}
	}
}

func TestBuildCoreUsersSlugCollisionResolved(t *testing.T) {
	store := profile.NewStore(t.TempDir())
	_ = store.SetAnswerTheme("discord:1", "Alice", profile.ThemeClear)
	_ = store.SetAnswerTheme("discord:2", "Alice", profile.ThemeClear)
	_ = store.SetAnswerTheme("discord:3", "Alice", profile.ThemeClear)
	out := buildCoreUsers(store, &config.Config{})
	if len(out) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(out))
	}
	slugs := map[string]bool{}
	for _, u := range out {
		if slugs[u.Slug] {
			t.Errorf("duplicate slug %q", u.Slug)
		}
		slugs[u.Slug] = true
	}
	// Should be alice, alice-2, alice-3
	if !slugs["alice"] || !slugs["alice-2"] || !slugs["alice-3"] {
		t.Errorf("expected alice / alice-2 / alice-3, got %v", slugs)
	}
}

func TestBuildCoreUsersEmpty(t *testing.T) {
	out := buildCoreUsers(profile.NewStore(t.TempDir()), &config.Config{})
	if len(out) != 0 {
		t.Errorf("expected empty list, got %v", out)
	}
}
