package main

import (
	"sort"
	"strings"
	"testing"
)

// Anything the UI hands to a write or a delete goes through these two
// functions first. A path that escapes the server root, or resolves to the
// root itself, has to be refused here — nothing downstream re-checks it.
func TestNormalizeRemotePath(t *testing.T) {
	ok := map[string]string{
		"/":                     "/",
		"":                      "", // handled by the error table below
		"/server.properties":    "/server.properties",
		"server.properties":     "/server.properties",
		"/plugins//Essentials":  "/plugins/Essentials",
		"/plugins/./config.yml": "/plugins/config.yml",
		"/plugins/x/../y.yml":   "/plugins/y.yml",
		"/a/b/c/../../d":        "/a/d",
		`\plugins\config.yml`:   "/plugins/config.yml",
		"/trailing/":            "/trailing",
		"/spaces in name.txt":   "/spaces in name.txt",
		"/unicode/日本語.yml":      "/unicode/日本語.yml",
	}
	for in, want := range ok {
		if in == "" {
			continue
		}
		got, err := normalizeRemotePath(in)
		if err != nil {
			t.Errorf("normalizeRemotePath(%q) errored: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("normalizeRemotePath(%q) = %q, want %q", in, got, want)
		}
	}

	bad := []string{"", "   ", "/a\x00b"}
	for _, in := range bad {
		if got, err := normalizeRemotePath(in); err == nil {
			t.Errorf("normalizeRemotePath(%q) should have failed, got %q", in, got)
		}
	}
}

// Clean() collapses traversal rather than allowing it out of the root. Assert
// that explicitly: the guarantee the delete path relies on is that no input
// produces a path outside "/".
func TestNormalizeNeverEscapesRoot(t *testing.T) {
	attempts := []string{
		"../../../etc/passwd",
		"/../../etc/passwd",
		"/plugins/../../../../etc/shadow",
		`..\..\windows\system32`,
		"/a/../..",
		"/..",
	}
	for _, in := range attempts {
		got, err := normalizeRemotePath(in)
		if err != nil {
			continue // refusing outright is also fine
		}
		if !strings.HasPrefix(got, "/") || strings.Contains(got, "..") {
			t.Errorf("normalizeRemotePath(%q) = %q, which is outside the server root", in, got)
		}
	}
}

func TestSplitRemote(t *testing.T) {
	cases := []struct {
		in       string
		dir      string
		name     string
		wantsErr bool
	}{
		{in: "/server.properties", dir: "/", name: "server.properties"},
		{in: "/plugins/Essentials/config.yml", dir: "/plugins/Essentials", name: "config.yml"},
		{in: "/plugins/", dir: "/", name: "plugins"},
		{in: "/", wantsErr: true},
		{in: "", wantsErr: true},
		{in: "/..", wantsErr: true},
		{in: "/a/..", wantsErr: true}, // resolves to the root
	}

	for _, c := range cases {
		dir, name, err := splitRemote(c.in)
		if c.wantsErr {
			if err == nil {
				t.Errorf("splitRemote(%q) should have failed, got %q %q", c.in, dir, name)
			}
			continue
		}
		if err != nil {
			t.Errorf("splitRemote(%q) errored: %v", c.in, err)
			continue
		}
		if dir != c.dir || name != c.name {
			t.Errorf("splitRemote(%q) = (%q, %q), want (%q, %q)", c.in, dir, name, c.dir, c.name)
		}
	}
}

// splitRemote and joinRemote have to be exact inverses; the delete groups
// paths by directory with one and rebuilds them with the other, and a mismatch
// there would report a different path than the one it removed.
func TestJoinRemoteInvertsSplitRemote(t *testing.T) {
	paths := []string{
		"/server.properties",
		"/plugins/config.yml",
		"/a/b/c/d/e.txt",
		"/one deep/two deep/three.yml",
	}
	for _, p := range paths {
		dir, name, err := splitRemote(p)
		if err != nil {
			t.Fatalf("splitRemote(%q): %v", p, err)
		}
		if got := joinRemote(dir, name); got != p {
			t.Errorf("joinRemote(splitRemote(%q)) = %q", p, got)
		}
	}
}

func TestClassifyPath(t *testing.T) {
	cases := []struct {
		path  string
		isDir bool
		want  string
	}{
		{"/server.properties", false, ProtectCritical},
		{"/SERVER.PROPERTIES", false, ProtectCritical}, // matched case-insensitively
		{"/plugins/Essentials/config.yml", false, ProtectCritical},
		{"/level.dat", false, ProtectCritical},
		{"/world", true, ProtectCritical},
		{"/plugins", true, ProtectCritical},
		{"/plugins/Essentials", true, ProtectSensitive}, // a directory, but not a known one
		{"/paper.jar", false, ProtectSensitive},
		{"/data/players.sqlite", false, ProtectSensitive},
		{"/logs/latest.log", false, ProtectNone},
		{"/readme.md", false, ProtectNone},
	}

	for _, c := range cases {
		got, _ := classifyPath(c.path, c.isDir)
		if got != c.want {
			t.Errorf("classifyPath(%q, dir=%v) = %q, want %q", c.path, c.isDir, got, c.want)
		}
	}
}

// Every directory is at least sensitive: deleting one is recursive, and the
// dialog escalates to a typed confirmation on that basis.
func TestEveryDirectoryIsAtLeastSensitive(t *testing.T) {
	for _, dir := range []string{"/anything", "/a/b/c", "/logs", "/tmp"} {
		if level, _ := classifyPath(dir, true); level == ProtectNone {
			t.Errorf("classifyPath(%q, dir=true) returned %q; a recursive delete must never be unflagged", dir, level)
		}
	}
}

// The token is what binds a confirmation to a selection. Different selections,
// and the same selection on a different server, must not share one.
func TestPlanTokenBindsServerAndPaths(t *testing.T) {
	base := planToken("srv1", []string{"/a.txt", "/b.txt"})

	same := planToken("srv1", []string{"/a.txt", "/b.txt"})
	if base != same {
		t.Fatal("the same selection produced two different tokens")
	}

	different := map[string]string{
		"other server":  planToken("srv2", []string{"/a.txt", "/b.txt"}),
		"extra path":    planToken("srv1", []string{"/a.txt", "/b.txt", "/c.txt"}),
		"fewer paths":   planToken("srv1", []string{"/a.txt"}),
		"other path":    planToken("srv1", []string{"/a.txt", "/c.txt"}),
		"empty server":  planToken("", []string{"/a.txt", "/b.txt"}),
		"joined naming": planToken("srv1", []string{"/a.txt/b.txt"}),
	}
	for what, token := range different {
		if token == base {
			t.Errorf("%s produced the same token as the original selection", what)
		}
	}
}

// The separator between paths keeps concatenation ambiguities out: without it
// {"/ab", "/c"} and {"/a", "/bc"} would hash the same.
func TestPlanTokenIsNotAmbiguous(t *testing.T) {
	if planToken("s", []string{"/ab", "/c"}) == planToken("s", []string{"/a", "/bc"}) {
		t.Fatal("two different selections share a token")
	}
}

func TestIsNotFound(t *testing.T) {
	if !isNotFound(errTest("API returned status 404: not here")) {
		t.Error("a 404 was not recognised")
	}
	if isNotFound(errTest("API returned status 500: daemon offline")) {
		t.Error("a 500 was treated as a missing file, which would let a write create one blindly")
	}
	if isNotFound(nil) {
		t.Error("nil was treated as not-found")
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		0:                 "0 B",
		512:               "512 B",
		1024:              "1.0 KB",
		100 * 1024 * 1024: "100.0 MB",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

// A selection containing both a folder and something inside it used to plan
// the inner path twice, then delete it twice — the second attempt failing
// against a parent that was already gone, which took the whole operation's
// recycle-bin copies with it.
func TestCollapseNested(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "child inside a selected folder is dropped",
			in:   []string{"/plugins", "/plugins/config.yml"},
			want: []string{"/plugins"},
		},
		{
			name: "grandchild too",
			in:   []string{"/plugins", "/plugins/Essentials/config.yml"},
			want: []string{"/plugins"},
		},
		{
			name: "siblings are all kept",
			in:   []string{"/logs", "/plugins", "/world"},
			want: []string{"/logs", "/plugins", "/world"},
		},
		{
			name: "a prefix that is not a path boundary is not nesting",
			in:   []string{"/plugins", "/pluginsX", "/plugins-old"},
			want: []string{"/plugins", "/plugins-old", "/pluginsX"},
		},
		{
			name: "the root swallows everything",
			in:   []string{"/", "/a", "/b/c"},
			want: []string{"/"},
		},
		{
			name: "deeper duplicates collapse to the shallowest",
			in:   []string{"/a", "/a/b", "/a/b/c", "/a/b/c/d"},
			want: []string{"/a"},
		},
	}

	for _, c := range cases {
		in := append([]string(nil), c.in...)
		sort.Strings(in)
		got := collapseNested(in)
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v, want %v", c.name, got, c.want)
				break
			}
		}
	}
}

// Every planned file has to be attributable to the root that was selected, or
// a failure on one root cannot roll back only its own copies.
func TestOwnerRoot(t *testing.T) {
	roots := []string{"/logs", "/plugins", "/plugins-old"}

	cases := map[string]string{
		"/plugins":                       "/plugins",
		"/plugins/config.yml":            "/plugins",
		"/plugins/Essentials/config.yml": "/plugins",
		"/plugins-old/config.yml":        "/plugins-old",
		"/logs/latest.log":               "/logs",
		"/world/level.dat":               "",
		"/pluginsX/thing.yml":            "",
	}

	for path, want := range cases {
		if got := ownerRoot(roots, path); got != want {
			t.Errorf("ownerRoot(%q) = %q, want %q", path, got, want)
		}
	}
}

// When one root nests inside another the deeper one wins, so a rollback
// scoped to it does not reach outside.
func TestOwnerRootPrefersTheDeepestMatch(t *testing.T) {
	roots := []string{"/a", "/a/b"}
	if got := ownerRoot(roots, "/a/b/c.txt"); got != "/a/b" {
		t.Errorf("ownerRoot returned %q, want the deeper /a/b", got)
	}
	if got := ownerRoot(roots, "/a/other.txt"); got != "/a" {
		t.Errorf("ownerRoot returned %q, want /a", got)
	}
}
