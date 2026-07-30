package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// buildSearchTree lays out root/a.txt, root/sub/b.txt, root/sub/deeper/c.txt,
// root/.hidden/d.txt — enough levels/hidden-ness to exercise both maxDepth
// pruning and the showHidden flag.
func buildSearchTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt")
	write("sub/b.txt")
	write("sub/deeper/c.txt")
	write(".hidden/d.txt")
	return root
}

func matchNames(root string, matches []searchMatch) []string {
	names := make([]string, len(matches))
	for i, m := range matches {
		rel, _ := filepath.Rel(root, m.path)
		names[i] = rel
	}
	sort.Strings(names)
	return names
}

func TestSearchWalkUnlimitedDepthFindsEverything(t *testing.T) {
	root := buildSearchTree(t)
	got := matchNames(root, searchWalk(root, "txt", -1, false))
	want := []string{"a.txt", filepath.Join("sub", "b.txt"), filepath.Join("sub", "deeper", "c.txt")}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestSearchWalkDepthZeroIsRootOnly(t *testing.T) {
	root := buildSearchTree(t)
	got := matchNames(root, searchWalk(root, "txt", 0, false))
	want := []string{"a.txt"}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("depth 0: got %v, want %v", got, want)
	}
}

func TestSearchWalkDepthOneStopsBeforeDeeper(t *testing.T) {
	root := buildSearchTree(t)
	got := matchNames(root, searchWalk(root, "txt", 1, false))
	want := []string{"a.txt", filepath.Join("sub", "b.txt")}
	sort.Strings(want)
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("depth 1: got %v, want %v", got, want)
	}
}

func TestSearchWalkHiddenExcludedByDefault(t *testing.T) {
	root := buildSearchTree(t)
	got := searchWalk(root, "d.txt", -1, false)
	if len(got) != 0 {
		t.Fatalf("hidden file should be excluded by default, got %v", got)
	}
	got = searchWalk(root, "d.txt", -1, true)
	if len(got) != 1 {
		t.Fatalf("hidden file should be found with showHidden=true, got %v", got)
	}
}

func TestListboxNamesKeepsPlainNamesWhenUnique(t *testing.T) {
	root := t.TempDir()
	matches := []searchMatch{
		{path: filepath.Join(root, "a.txt"), name: "a.txt"},
		{path: filepath.Join(root, "sub", "b.txt"), name: "b.txt"},
	}
	names := listboxNames(root, matches)
	if len(names) != 2 {
		t.Fatalf("got %d names, want 2: %+v", len(names), names)
	}
	if names["a.txt"] != matches[0].path || names["b.txt"] != matches[1].path {
		t.Fatalf("names = %+v, want plain basenames mapped to their real paths", names)
	}
}

func TestListboxNamesDisambiguatesCollidingBasenames(t *testing.T) {
	root := t.TempDir()
	p1 := filepath.Join(root, "one", "readme.txt")
	p2 := filepath.Join(root, "two", "readme.txt")
	matches := []searchMatch{
		{path: p1, name: "readme.txt"},
		{path: p2, name: "readme.txt"},
	}
	names := listboxNames(root, matches)
	if len(names) != 2 {
		t.Fatalf("got %d names, want 2 distinct entries for two different real files: %+v", len(names), names)
	}
	seen := map[string]bool{}
	for _, real := range names {
		if real != p1 && real != p2 {
			t.Fatalf("unexpected real path %q in %+v", real, names)
		}
		seen[real] = true
	}
	if len(seen) != 2 {
		t.Fatalf("both real files should be reachable under distinct names, got %+v", names)
	}
}

func TestSearchDepthValueMapping(t *testing.T) {
	cases := map[string]int{
		"Unlimited":        -1,
		"Just this folder": 0,
		"1 level deep":     1,
		"2 levels deep":    2,
		"3 levels deep":    3,
		"5 levels deep":    5,
		"10 levels deep":   10,
		"anything else":    -1,
	}
	for label, want := range cases {
		if got := searchDepthValue(label); got != want {
			t.Errorf("searchDepthValue(%q) = %d, want %d", label, got, want)
		}
	}
}
