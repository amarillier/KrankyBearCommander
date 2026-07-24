package main

import "testing"

func TestStemExt(t *testing.T) {
	cases := []struct {
		name     string
		isDir    bool
		wantStem string
		wantExt  string
	}{
		{"photo.jpg", false, "photo", ".jpg"},
		{"archive.tar.gz", false, "archive.tar", ".gz"},
		{"README", false, "README", ""},
		{".gitignore", false, ".gitignore", ""},
		{"my.project", true, "my.project", ""},
	}
	for _, c := range cases {
		stem, ext := stemExt(c.name, c.isDir)
		if stem != c.wantStem || ext != c.wantExt {
			t.Errorf("stemExt(%q, %v) = (%q, %q), want (%q, %q)", c.name, c.isDir, stem, ext, c.wantStem, c.wantExt)
		}
	}
}
