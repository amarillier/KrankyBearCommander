package sftpfs

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"

	"commander/internal/connections"
)

// writeTestKey generates a throwaway unencrypted RSA private key PEM file
// for exercising authMethod's key-parsing path without a real server.
func writeTestKey(t *testing.T, dir string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}
	path := filepath.Join(dir, "id_rsa")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAuthMethodNoKeyNoPasswordErrors(t *testing.T) {
	_, err := authMethod(&connections.Connection{}, "")
	if err == nil {
		t.Fatal("expected an error when neither a key nor a password is configured")
	}
}

func TestAuthMethodPasswordOnly(t *testing.T) {
	auth, err := authMethod(&connections.Connection{}, "s3cr3t")
	if err != nil {
		t.Fatal(err)
	}
	if auth == nil {
		t.Fatal("expected a non-nil auth method")
	}
}

func TestAuthMethodSSHKeyUnencrypted(t *testing.T) {
	keyPath := writeTestKey(t, t.TempDir())
	auth, err := authMethod(&connections.Connection{SSHKeyPath: keyPath}, "")
	if err != nil {
		t.Fatal(err)
	}
	if auth == nil {
		t.Fatal("expected a non-nil auth method")
	}
}

// TestAuthMethodFallsBackWhenKeyIsNotActuallyEncrypted is a regression test:
// a stored passphrase that doesn't apply to this key (stale from editing the
// connection, or a keychain entry that differs machine-to-machine — this
// bit Allan on Windows while the same connection worked fine on Mac)
// shouldn't fail a key that in fact has no passphrase at all.
func TestAuthMethodFallsBackWhenKeyIsNotActuallyEncrypted(t *testing.T) {
	keyPath := writeTestKey(t, t.TempDir())
	auth, err := authMethod(&connections.Connection{SSHKeyPath: keyPath}, "some-stale-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if auth == nil {
		t.Fatal("expected a non-nil auth method")
	}
}

// TestAuthMethodStripsPubSuffixToFindPrivateKey is a regression test: a
// real-world `ssh -i ~/.ssh/id_rsa.pub ...` alias (like Allan's) points at
// the PUBLIC key and still works with OpenSSH's own client — authMethod
// must accept the same convention rather than trying (and failing) to parse
// the public key blob as a private key.
func TestAuthMethodStripsPubSuffixToFindPrivateKey(t *testing.T) {
	dir := t.TempDir()
	privatePath := writeTestKey(t, dir) // dir/id_rsa
	if err := os.WriteFile(privatePath+".pub", []byte("ssh-rsa not-a-real-public-key-blob\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	auth, err := authMethod(&connections.Connection{SSHKeyPath: privatePath + ".pub"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if auth == nil {
		t.Fatal("expected a non-nil auth method")
	}
}

func TestAuthMethodSSHKeyMissingFileErrors(t *testing.T) {
	_, err := authMethod(&connections.Connection{SSHKeyPath: filepath.Join(t.TempDir(), "does-not-exist")}, "")
	if err == nil {
		t.Fatal("expected an error reading a nonexistent key file")
	}
}

func TestFingerprintIsStableAndHex(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	a := Fingerprint(signer.PublicKey())
	b := Fingerprint(signer.PublicKey())
	if a != b {
		t.Fatalf("Fingerprint should be deterministic for the same key: %q != %q", a, b)
	}
	if len(a) != 64 { // SHA-256, hex-encoded
		t.Fatalf("Fingerprint length = %d, want 64 (SHA-256 hex)", len(a))
	}
}

func TestPresentedAndInternalPathRoundTrip(t *testing.T) {
	fs := &FS{prefix: "sftp://allan@example.com:22"}
	presented := fs.Presented("/home/allan/docs")
	if presented != "sftp://allan@example.com:22/home/allan/docs" {
		t.Fatalf("Presented = %q", presented)
	}
	if got := fs.internalPath(presented); got != "/home/allan/docs" {
		t.Fatalf("internalPath = %q, want /home/allan/docs", got)
	}
	// Presented("") / Presented("/") both mean the connection's own top.
	if fs.Presented("") != fs.Presented("/") {
		t.Fatalf("Presented(\"\") should equal Presented(\"/\")")
	}
	if got := fs.internalPath(fs.prefix); got != "/" {
		t.Fatalf("internalPath(prefix) = %q, want /", got)
	}
}

func TestJoinDoesNotCollapseSchemeSlashes(t *testing.T) {
	fs := &FS{prefix: "sftp://allan@example.com:22"}
	root := fs.Presented("/")
	got := fs.Join(root, "file.txt")
	want := "sftp://allan@example.com:22/file.txt"
	if got != want {
		t.Fatalf("Join(root, file.txt) = %q, want %q (the \"://\" must survive intact)", got, want)
	}
}

func TestJoinOfUnrelatedPathFallsBackToPlainJoin(t *testing.T) {
	fs := &FS{prefix: "sftp://allan@example.com:22"}
	got := fs.Join("/some/local/dir", "file.txt")
	want := filepath.Join("/some/local/dir", "file.txt")
	// path.Join always uses "/" — filepath.Join matches on non-Windows; this
	// assertion only needs the "not one of ours -> plain join" behavior,
	// which path.Join and filepath.Join agree on for forward-slash input.
	if got != want && got != "/some/local/dir/file.txt" {
		t.Fatalf("Join of an unrelated path = %q", got)
	}
}

func TestDirWalksUpAndStopsAtRemoteRoot(t *testing.T) {
	fs := &FS{prefix: "sftp://allan@example.com:22"}
	deep := fs.Presented("/home/allan/docs")
	up1 := fs.Dir(deep)
	if up1 != fs.Presented("/home/allan") {
		t.Fatalf("Dir(deep) = %q, want %q", up1, fs.Presented("/home/allan"))
	}
	up2 := fs.Dir(up1)
	if up2 != fs.Presented("/home") {
		t.Fatalf("Dir(up1) = %q, want %q", up2, fs.Presented("/home"))
	}
	up3 := fs.Dir(up2)
	if up3 != fs.Presented("/") {
		t.Fatalf("Dir(up2) = %q, want the remote root %q", up3, fs.Presented("/"))
	}
	// At the true top, Dir returns itself — nowhere further up.
	if got := fs.Dir(up3); got != up3 {
		t.Fatalf("Dir(root) = %q, want itself %q (no further parent)", got, up3)
	}
}

func TestIsInside(t *testing.T) {
	fs := &FS{prefix: "sftp://allan@example.com:22"}
	if !fs.IsInside(fs.Presented("/")) {
		t.Fatal("the connection's own root should be inside itself")
	}
	if !fs.IsInside(fs.Presented("/home/allan")) {
		t.Fatal("a path within the connection should be inside it")
	}
	if fs.IsInside("/Users/allan") {
		t.Fatal("a real local path should not be considered inside a remote connection")
	}
	if fs.IsInside("sftp://allan@example.com:22suffix") {
		t.Fatal("a sibling connection sharing this prefix as a string prefix should not be considered inside")
	}
}

func TestCloseWithNilConnsDoesNotPanic(t *testing.T) {
	fs := &FS{}
	if err := fs.Close(); err != nil {
		t.Fatalf("Close() on a zero-value FS = %v, want nil", err)
	}
}
