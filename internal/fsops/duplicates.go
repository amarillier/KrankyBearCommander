package fsops

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// DuplicateGroup is 2+ files under a FindDuplicates root whose content is
// byte-for-byte identical (same size, same sha256 hash).
type DuplicateGroup struct {
	Size  int64
	Paths []string
}

// FindDuplicates recursively walks root, groups every regular file by size
// (a cheap pre-filter — files that can't possibly match anything are never
// hashed), then hashes every file in a size group that has more than one
// member and groups those by hash. Empty (size 0) files are excluded — every
// empty file in a tree would otherwise collapse into one meaningless
// "duplicate" group. maxDepth caps how many directory levels below root are
// descended into (0 = root's direct contents only), or -1 for unlimited —
// same convention as search_ui.go's searchWalk. progress is called with a
// phase label ("Scanning: <name>" during the walk, "Hashing: <name>" during
// hashing) and the path currently being processed; a false return cancels
// early with ErrCancelled. Groups are sorted by total reclaimable space
// (Size * (len(Paths)-1)) descending, so the most impactful duplicates
// surface first.
func FindDuplicates(root string, showHidden bool, maxDepth int, progress func(phase, currentPath string) bool) ([]DuplicateGroup, error) {
	if progress == nil {
		progress = func(string, string) bool { return true }
	}

	type fileInfo struct {
		path string
		size int64
	}
	bySize := make(map[int64][]fileInfo)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries, keep walking
		}
		if path == root {
			return nil
		}
		if maxDepth >= 0 {
			rel, relErr := filepath.Rel(root, path)
			if relErr == nil && strings.Count(rel, string(filepath.Separator)) > maxDepth {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if !showHidden && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if !progress("Scanning: "+d.Name(), path) {
				return ErrCancelled
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Size() == 0 {
			return nil
		}
		if !progress("Scanning: "+d.Name(), path) {
			return ErrCancelled
		}
		bySize[info.Size()] = append(bySize[info.Size()], fileInfo{path, info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}

	var groups []DuplicateGroup
	for size, files := range bySize {
		if len(files) < 2 {
			continue
		}
		byHash := make(map[string][]string, len(files))
		for _, f := range files {
			h, err := HashFile(f.path, func(int64) bool {
				return progress("Hashing: "+filepath.Base(f.path), f.path)
			})
			if err != nil {
				if err == ErrCancelled {
					return nil, ErrCancelled
				}
				continue // unreadable file — skip it, don't fail the whole scan
			}
			byHash[h] = append(byHash[h], f.path)
		}
		for _, paths := range byHash {
			if len(paths) < 2 {
				continue
			}
			groups = append(groups, DuplicateGroup{Size: size, Paths: paths})
		}
	}

	sort.Slice(groups, func(i, j int) bool {
		wi := groups[i].Size * int64(len(groups[i].Paths)-1)
		wj := groups[j].Size * int64(len(groups[j].Paths)-1)
		return wi > wj
	})
	return groups, nil
}
