package fileagentfs

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"commander/internal/connections"
)

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func splitAddr(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	return host, port
}

// connectHelper starts a fake FileAgent server rooted at root and connects
// this package's real client to it — the server is closed automatically
// via t.Cleanup, the *FS is the caller's to Close.
func connectHelper(t *testing.T, root, psk string) *FS {
	t.Helper()
	srv := startTestServer(t, root, psk)
	t.Cleanup(srv.close)
	host, port := splitAddr(t, srv.addr())
	conn := &connections.Connection{Host: host, Port: port, FileAgentTLSPin: srv.pin}
	fs, err := Connect(conn, psk)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

// ── pure logic ───────────────────────────────────────────────────────────

func TestNormalizeVirtualPath(t *testing.T) {
	cases := map[string]string{
		"":            "/",
		"/":           "/",
		"foo":         "/foo",
		"/foo/bar":    "/foo/bar",
		`foo\bar`:     "/foo/bar",
		"/foo/../bar": "/bar",
	}
	for in, want := range cases {
		if got := normalizeVirtualPath(in); got != want {
			t.Errorf("normalizeVirtualPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizePin(t *testing.T) {
	if got, want := normalizePin("AB:CD EF"), "abcdef"; got != want {
		t.Fatalf("normalizePin = %q, want %q", got, want)
	}
}

func TestPresentedAndInternalPathRoundTrip(t *testing.T) {
	fs := &FS{prefix: "fileagent://192.168.1.50:9742"}
	presented := fs.Presented("/docs/report.txt")
	if presented != "fileagent://192.168.1.50:9742/docs/report.txt" {
		t.Fatalf("Presented = %q", presented)
	}
	if got := fs.internalPath(presented); got != "/docs/report.txt" {
		t.Fatalf("internalPath = %q, want /docs/report.txt", got)
	}
	if fs.Presented("") != fs.Presented("/") {
		t.Fatal("Presented(\"\") should equal Presented(\"/\")")
	}
}

func TestJoinDoesNotCollapseSchemeSlashes(t *testing.T) {
	fs := &FS{prefix: "fileagent://192.168.1.50:9742"}
	root := fs.Presented("/")
	got := fs.Join(root, "file.txt")
	want := "fileagent://192.168.1.50:9742/file.txt"
	if got != want {
		t.Fatalf("Join(root, file.txt) = %q, want %q", got, want)
	}
}

func TestDirWalksUpAndStopsAtRoot(t *testing.T) {
	fs := &FS{prefix: "fileagent://192.168.1.50:9742"}
	deep := fs.Presented("/a/b")
	up1 := fs.Dir(deep)
	if up1 != fs.Presented("/a") {
		t.Fatalf("Dir(deep) = %q, want %q", up1, fs.Presented("/a"))
	}
	up2 := fs.Dir(up1)
	if up2 != fs.Presented("/") {
		t.Fatalf("Dir(up1) = %q, want root %q", up2, fs.Presented("/"))
	}
	if got := fs.Dir(up2); got != up2 {
		t.Fatalf("Dir(root) = %q, want itself %q", got, up2)
	}
}

func TestIsInside(t *testing.T) {
	fs := &FS{prefix: "fileagent://192.168.1.50:9742"}
	if !fs.IsInside(fs.Presented("/")) {
		t.Fatal("the connection's own root should be inside itself")
	}
	if fs.IsInside("/Users/allan") {
		t.Fatal("a real local path should not be considered inside a remote connection")
	}
}

func TestCloseWithNilConnDoesNotPanic(t *testing.T) {
	fs := &FS{}
	if err := fs.Close(); err != nil {
		t.Fatalf("Close() on a zero-value FS = %v, want nil", err)
	}
}

// ── against a real (fake) server ────────────────────────────────────────

func TestConnectAuthSucceeds(t *testing.T) {
	fs := connectHelper(t, t.TempDir(), "s3cr3t")
	defer fs.Close()
}

func TestConnectWrongPSKFails(t *testing.T) {
	root := t.TempDir()
	srv := startTestServer(t, root, "s3cr3t")
	t.Cleanup(srv.close)
	host, port := splitAddr(t, srv.addr())
	conn := &connections.Connection{Host: host, Port: port, FileAgentTLSPin: srv.pin}
	if _, err := Connect(conn, "wrong"); err == nil {
		t.Fatal("expected authentication failure with the wrong PSK")
	}
}

func TestConnectWrongPinFails(t *testing.T) {
	root := t.TempDir()
	srv := startTestServer(t, root, "s3cr3t")
	t.Cleanup(srv.close)
	host, port := splitAddr(t, srv.addr())
	conn := &connections.Connection{Host: host, Port: port, FileAgentTLSPin: strings.Repeat("0", 64)}
	if _, err := Connect(conn, "s3cr3t"); err == nil {
		t.Fatal("expected a TLS handshake failure with a fingerprint that doesn't match")
	}
}

func TestReadDirListsRealFiles(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.txt"), "hello")
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	fs := connectHelper(t, root, "pw")
	defer fs.Close()

	entries, err := fs.ReadDir(fs.Presented("/"))
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]bool{}
	for _, e := range entries {
		byName[e.Name] = e.IsDir
	}
	if isDir, ok := byName["a.txt"]; !ok || isDir {
		t.Fatalf("expected a.txt as a file entry, got %+v", byName)
	}
	if isDir, ok := byName["sub"]; !ok || !isDir {
		t.Fatalf("expected sub as a directory entry, got %+v", byName)
	}
}

func TestDownloadSingleFile(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "hello.txt"), "hello world")
	fs := connectHelper(t, root, "pw")
	defer fs.Close()

	destDir := t.TempDir()
	if err := fs.Download([]string{fs.Presented("/hello.txt")}, destDir, nil, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destDir, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello world" {
		t.Fatalf("got %q, want %q", got, "hello world")
	}
}

func TestDownloadDirectoryRecursive(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "proj", "top.txt"), "top")
	mustWriteFile(t, filepath.Join(root, "proj", "sub", "deep.txt"), "deep")
	fs := connectHelper(t, root, "pw")
	defer fs.Close()

	destDir := t.TempDir()
	if err := fs.Download([]string{fs.Presented("/proj")}, destDir, nil, nil); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(destDir, "proj", "top.txt")); err != nil || string(data) != "top" {
		t.Fatalf("proj/top.txt = %q, %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(destDir, "proj", "sub", "deep.txt")); err != nil || string(data) != "deep" {
		t.Fatalf("proj/sub/deep.txt = %q, %v", data, err)
	}
}

func TestUploadSingleFile(t *testing.T) {
	root := t.TempDir()
	fs := connectHelper(t, root, "pw")
	defer fs.Close()

	srcDir := t.TempDir()
	mustWriteFile(t, filepath.Join(srcDir, "up.txt"), "uploaded")

	if err := fs.Upload([]string{filepath.Join(srcDir, "up.txt")}, fs.Presented("/"), nil, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "up.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "uploaded" {
		t.Fatalf("got %q, want uploaded", got)
	}
}

func TestUploadDirectoryRecursive(t *testing.T) {
	root := t.TempDir()
	fs := connectHelper(t, root, "pw")
	defer fs.Close()

	srcDir := t.TempDir()
	mustWriteFile(t, filepath.Join(srcDir, "proj", "top.txt"), "top")
	mustWriteFile(t, filepath.Join(srcDir, "proj", "sub", "deep.txt"), "deep")

	if err := fs.Upload([]string{filepath.Join(srcDir, "proj")}, fs.Presented("/"), nil, nil); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "proj", "top.txt")); err != nil || string(data) != "top" {
		t.Fatalf("proj/top.txt = %q, %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "proj", "sub", "deep.txt")); err != nil || string(data) != "deep" {
		t.Fatalf("proj/sub/deep.txt = %q, %v", data, err)
	}
}

func TestMkdirRemoveRename(t *testing.T) {
	root := t.TempDir()
	fs := connectHelper(t, root, "pw")
	defer fs.Close()

	if err := fs.Mkdir(fs.Presented("/newdir")); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(root, "newdir")); err != nil || !info.IsDir() {
		t.Fatalf("newdir should exist as a directory: %v", err)
	}

	mustWriteFile(t, filepath.Join(root, "renameme.txt"), "x")
	if err := fs.Rename(fs.Presented("/renameme.txt"), fs.Presented("/renamed.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "renamed.txt")); err != nil {
		t.Fatal("renamed.txt should exist after Rename")
	}

	if err := fs.Remove(fs.Presented("/renamed.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "renamed.txt")); err == nil {
		t.Fatal("renamed.txt should no longer exist after Remove")
	}
}

func TestStatDerivedFromListing(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.txt"), "hello")
	fs := connectHelper(t, root, "pw")
	defer fs.Close()

	entry, err := fs.Stat(fs.Presented("/a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if entry.IsDir || entry.Size != 5 {
		t.Fatalf("Stat(a.txt) = %+v, want a 5-byte file", entry)
	}
}

func TestOpenReadsWholeFile(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.txt"), "hello world")
	fs := connectHelper(t, root, "pw")
	defer fs.Close()

	rc, err := fs.Open(fs.Presented("/a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	data := make([]byte, 11)
	if _, err := rc.Read(data); err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello world" {
		t.Fatalf("got %q, want hello world", data)
	}
}
