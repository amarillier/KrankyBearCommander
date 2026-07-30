package smbfs

import "testing"

func TestParseShareAndPathBareShareName(t *testing.T) {
	share, within, err := parseShareAndPath("Users")
	if err != nil {
		t.Fatal(err)
	}
	if share != "Users" || within != "/" {
		t.Fatalf("got (%q, %q), want (Users, /)", share, within)
	}
}

func TestParseShareAndPathWithSubpathForwardSlash(t *testing.T) {
	share, within, err := parseShareAndPath("Users/allan/docs")
	if err != nil {
		t.Fatal(err)
	}
	if share != "Users" || within != "/allan/docs" {
		t.Fatalf("got (%q, %q), want (Users, /allan/docs)", share, within)
	}
}

func TestParseShareAndPathWithSubpathBackslash(t *testing.T) {
	share, within, err := parseShareAndPath(`Users\allan\docs`)
	if err != nil {
		t.Fatal(err)
	}
	if share != "Users" || within != "/allan/docs" {
		t.Fatalf("got (%q, %q), want (Users, /allan/docs)", share, within)
	}
}

func TestParseShareAndPathUNCFormSkipsServerSegment(t *testing.T) {
	share, within, err := parseShareAndPath(`\\nas\Users\allan`)
	if err != nil {
		t.Fatal(err)
	}
	if share != "Users" || within != "/allan" {
		t.Fatalf("got (%q, %q), want (Users, /allan) — the UNC server segment should be skipped, not treated as the share", share, within)
	}
}

func TestParseShareAndPathEmptyErrors(t *testing.T) {
	if _, _, err := parseShareAndPath(""); err == nil {
		t.Fatal("expected an error for a connection with no share name configured")
	}
	if _, _, err := parseShareAndPath("/"); err == nil {
		t.Fatal("expected an error for a bare separator with no share name")
	}
}

func TestPresentedAndInternalPathRoundTrip(t *testing.T) {
	fs := &FS{prefix: "smb://allan@192.168.1.50:445/Users"}
	presented := fs.Presented("/allan/docs")
	if presented != "smb://allan@192.168.1.50:445/Users/allan/docs" {
		t.Fatalf("Presented = %q", presented)
	}
	if got := fs.internalPath(presented); got != "/allan/docs" {
		t.Fatalf("internalPath = %q, want /allan/docs", got)
	}
	if fs.Presented("") != fs.Presented("/") {
		t.Fatal("Presented(\"\") should equal Presented(\"/\")")
	}
}

func TestShareRelativeStripsLeadingSlashForGoSMB2(t *testing.T) {
	// go-smb2's own path validation rejects a leading separator outright —
	// shareRelative must never hand one back.
	fs := &FS{prefix: "smb://allan@192.168.1.50:445/Users"}
	if got := fs.shareRelative(fs.Presented("/")); got != "" {
		t.Fatalf("shareRelative(root) = %q, want empty string", got)
	}
	if got := fs.shareRelative(fs.Presented("/allan/docs")); got != "allan/docs" {
		t.Fatalf("shareRelative = %q, want allan/docs (no leading slash)", got)
	}
}

func TestStartPathReflectsParsedRemotePath(t *testing.T) {
	fs := &FS{prefix: "smb://allan@192.168.1.50:445/Users", startPath: "/allan"}
	if got, want := fs.StartPath(), "smb://allan@192.168.1.50:445/Users/allan"; got != want {
		t.Fatalf("StartPath() = %q, want %q", got, want)
	}
}

func TestJoinDoesNotCollapseSchemeSlashes(t *testing.T) {
	fs := &FS{prefix: "smb://allan@192.168.1.50:445/Users"}
	root := fs.Presented("/")
	got := fs.Join(root, "file.txt")
	want := "smb://allan@192.168.1.50:445/Users/file.txt"
	if got != want {
		t.Fatalf("Join(root, file.txt) = %q, want %q", got, want)
	}
}

func TestDirWalksUpAndStopsAtShareRoot(t *testing.T) {
	fs := &FS{prefix: "smb://allan@192.168.1.50:445/Users"}
	deep := fs.Presented("/allan/docs")
	up1 := fs.Dir(deep)
	if up1 != fs.Presented("/allan") {
		t.Fatalf("Dir(deep) = %q, want %q", up1, fs.Presented("/allan"))
	}
	up2 := fs.Dir(up1)
	if up2 != fs.Presented("/") {
		t.Fatalf("Dir(up1) = %q, want the share root %q", up2, fs.Presented("/"))
	}
	if got := fs.Dir(up2); got != up2 {
		t.Fatalf("Dir(root) = %q, want itself %q (no further parent)", got, up2)
	}
}

func TestIsInside(t *testing.T) {
	fs := &FS{prefix: "smb://allan@192.168.1.50:445/Users"}
	if !fs.IsInside(fs.Presented("/")) {
		t.Fatal("the connection's own root should be inside itself")
	}
	if !fs.IsInside(fs.Presented("/allan")) {
		t.Fatal("a path within the connection should be inside it")
	}
	if fs.IsInside("/Users/allan") {
		t.Fatal("a real local path should not be considered inside a remote connection")
	}
}

func TestCloseWithNilFieldsDoesNotPanic(t *testing.T) {
	fs := &FS{}
	if err := fs.Close(); err != nil {
		t.Fatalf("Close() on a zero-value FS = %v, want nil", err)
	}
}
