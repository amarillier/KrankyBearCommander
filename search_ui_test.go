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
