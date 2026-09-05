package main

// The embedded frontend has to hold everything the window asks for.
//
// The embed is a list of patterns rather than the whole directory, because the
// whole directory included node_modules — 19 MB of dev dependencies, esbuild's
// own executable among them, inside a binary that gets shipped. The cost of
// that is a list which can fall behind: add a script to index.html, forget the
// pattern, and the window loads a blank page in the built app while working
// perfectly in dev.
//
// So this reads index.html out of the embedded copy and checks that every
// local thing it references is in there too.

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

func TestEveryLocalAssetIsEmbedded(t *testing.T) {
	index, err := assets.ReadFile("frontend/index.html")
	if err != nil {
		t.Fatalf("index.html is not embedded at all: %v", err)
	}

	refs := regexp.MustCompile(`(?:src|href)="([^"]+)"`).FindAllStringSubmatch(string(index), -1)
	if len(refs) == 0 {
		t.Fatal("found no references in index.html; the pattern is wrong, not the page")
	}

	checked := 0
	for _, ref := range refs {
		target := ref[1]
		// Only what is served from here. A CDN or a font is somebody else's.
		if !strings.HasPrefix(target, "./") {
			continue
		}
		path := "frontend/" + strings.TrimPrefix(target, "./")
		if _, err := fs.Stat(assets, path); err != nil {
			t.Errorf("index.html loads %s, which is not in the embedded assets — "+
				"add a pattern for it to the //go:embed list in main.go", target)
			continue
		}
		checked++
	}

	if checked < 10 {
		t.Errorf("only %d local assets checked; index.html loads far more than that, "+
			"so this test is not looking at what it thinks it is", checked)
	}
}

// The whole point of the pattern list: node_modules must not be in the binary.
func TestNodeModulesIsNotShipped(t *testing.T) {
	if _, err := fs.Stat(assets, "frontend/node_modules"); err == nil {
		t.Error("frontend/node_modules is embedded in the binary; the //go:embed " +
			"patterns in main.go have gone back to sweeping the whole directory")
	}
}
