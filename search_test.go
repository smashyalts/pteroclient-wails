package main

// What the search box accepts, and what it does with it.

import (
	"strings"
	"testing"
)

func TestSearchMatcher(t *testing.T) {
	cases := []struct {
		query string
		name  string
		want  bool
		why   string
	}{
		// Plain text is a substring, either case.
		{"config", "config.yml", true, "the obvious one"},
		{"CONFIG", "config.yml", true, "typing in caps still finds it"},
		{"config", "SubConfigHolder.java", true, "a substring, not a prefix"},
		{"yml", "config.yaml", false, "not there"},

		// A glob is the whole name, which is what * means to anyone typing it.
		{"*.yml", "config.yml", true, "the reason globs are here"},
		{"*.yml", "config.yaml", false, "a different extension"},
		{"*.yml", "yml.jar", false, "an unanchored glob would have matched this"},
		{"*.YML", "config.yml", true, "case does not matter in a glob either"},
		{"config.*", "config.yml", true, "the other way round"},
		{"?onfig.yml", "config.yml", true, "one character"},
		{"?onfig.yml", "onfig.yml", false, "? is exactly one, not none"},
		// The dot is a literal, not "any character".
		{"config.yml", "configXyml", false, "a plain query is not a pattern"},
		{"*.y?l", "configXyml", false, "the dot in a glob is still a dot"},

		// re: is the way in for a real pattern.
		{`re:^config\.(yml|yaml)$`, "config.yml", true, "alternation"},
		{`re:^config\.(yml|yaml)$`, "config.yaml", true, "the other branch"},
		{`re:^config\.(yml|yaml)$`, "myconfig.yml", false, "anchored at the front"},
		{`re:\d+\.jar`, "Essentials-2.19.jar", true, "digits"},
		{`RE:\d+\.jar`, "Essentials-2.19.jar", true, "the prefix is not case sensitive"},
		{`re:ENTITY`, "entity.json", true, "patterns ignore case by default"},
		{`re:(?-i)ENTITY`, "entity.json", false, "and can be told not to"},
	}

	for _, c := range cases {
		matcher, err := newSearchMatcher(c.query)
		if err != nil {
			t.Errorf("%q: %v", c.query, err)
			continue
		}
		if got := matcher.match(c.name); got != c.want {
			t.Errorf("%q against %q = %v, want %v — %s", c.query, c.name, got, c.want, c.why)
		}
	}
}

func TestSearchMatcherRefusals(t *testing.T) {
	for _, query := range []string{"", "   ", "re:", "re:   ", `re:[unclosed`, "re:(" } {
		if _, err := newSearchMatcher(query); err == nil {
			t.Errorf("%q was accepted; a query that cannot work has to say so", query)
		}
	}
	if _, err := newSearchMatcher(strings.Repeat("a", searchMaxPattern+1)); err == nil {
		t.Error("a pattern over the length limit was accepted")
	}
}

// containsFold replaced strings.Contains(strings.ToLower(name), needle), which
// allocated a copy of every name a search looked at.
func TestContainsFoldMatchesTheAllocatingVersion(t *testing.T) {
	names := []string{
		"config.yml", "CONFIG.YML", "Essentials-2.19.jar", "", "a",
		"paper-1.20.4.jar", "PlayerWarpsGUI", ".paper-remapped", "wörld",
	}
	needles := []string{"", "config", "jar", "gui", "wörld", "z", "playerwarps", "a"}

	for _, name := range names {
		for _, needle := range needles {
			want := strings.Contains(strings.ToLower(name), needle)
			if got := containsFold(name, needle); got != want {
				t.Errorf("containsFold(%q, %q) = %v, want %v", name, needle, got, want)
			}
		}
	}
}

func BenchmarkContainsFold(b *testing.B) {
	names := make([]string, 0, 5000)
	for i := 0; i < 5000; i++ {
		names = append(names, "SomePlugin-1.20.4-SNAPSHOT.jar")
	}
	needle := "snapshot"

	b.ResetTimer()
	b.ReportAllocs()
	hits := 0
	for i := 0; i < b.N; i++ {
		for _, name := range names {
			if containsFold(name, needle) {
				hits++
			}
		}
	}
	if hits == 0 {
		b.Fatal("matched nothing")
	}
}
