// Package sftpfs is an SFTP-backed vfs.FileSystem, so a pane can browse a
// remote server exactly like a real directory (see the Connections
// manager, connections_ui.go). Auth selection is ported from
// KrankyBearFileMover's own SFTP connection code; host-key verification is
// added fresh — FileMover's skips it entirely (ssh.InsecureIgnoreHostKey),
// this package does real SSH trust-on-first-use instead (see ProbeHostKey
// and Connect).
//
// Paths this package hands out and accepts ("presented" paths) are
// "sftp://user@host:port" followed by the remote's own "/"-separated path
// — e.g. "sftp://allan@example.com:22" (this connection's own top) or
// "sftp://allan@example.com:22/home/allan/docs". This lets every existing
// caller that treats a tab's current path as an ordinary string keep
// working, the same convention zipfs/listboxfs already established.
// Because that scheme prefix contains "//" (which path.Clean/filepath.Clean
// would collapse if run over the WHOLE string), Join/Dir below deliberately
// never hand the full presented string to the "path" package — only ever
// the bare remote path, once the prefix has been stripped off.
package sftpfs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"commander/internal/connections"
	"commander/internal/fsops"
	"commander/internal/vfs"
)

// FS is one open SFTP connection.
type FS struct {
	sshConn  *ssh.Client
	sftpConn *sftp.Client
	prefix   string // "sftp://user@host:port" — no trailing slash, see the package doc
}

// Fingerprint returns key's SHA-256 fingerprint as lowercase hex — the same
// value ProbeHostKey reports and Connect verifies against.
func Fingerprint(key ssh.PublicKey) string {
	sum := sha256.Sum256(key.Marshal())
	return hex.EncodeToString(sum[:])
}

func addr(host string, port int) string {
	if port == 0 {
		port = 22
	}
	return fmt.Sprintf("%s:%d", host, port)
}

// ProbeHostKey dials just far enough to learn the remote's host-key
// fingerprint, then disconnects without ever authenticating — used to show
// the user a trust prompt BEFORE the real Connect, rather than trying to
// block a confirmation dialog from inside the ssh package's own synchronous
// handshake callback (which would risk exactly the kind of Fyne-threading
// deadlock this app has been bitten by before, per CLAUDE.md). It is the
// caller's job to compare the returned fingerprint against any
// previously-trusted one and decide whether to save it and proceed.
func ProbeHostKey(host string, port int) (string, error) {
	var fingerprint string
	config := &ssh.ClientConfig{
		User:    "probe",
		Timeout: 10 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			fingerprint = Fingerprint(key)
			return nil // always accept — this connection only ever reads the key, never authenticates
		},
	}
	conn, err := ssh.Dial("tcp", addr(host, port), config)
	if err == nil {
		conn.Close()
		return fingerprint, nil
	}
	// With no auth methods offered, the handshake is expected to fail right
	// after the host-key check — that's fine, the fingerprint was already
	// captured by then. Only report an error if we truly never got one
	// (e.g. the TCP connection itself failed, or the host is unreachable).
	if fingerprint != "" {
		return fingerprint, nil
	}
	return "", fmt.Errorf("connect to %s: %w", addr(host, port), err)
}

// authMethod selects conn's ONE configured auth method — an SSH key if
// SSHKeyPath is set (secret is its passphrase, empty if the key has none),
// otherwise a login password (secret itself). Ported from
// KrankyBearFileMover's SFTPConnection.Connect, simplified to one method at
// a time since this app stores exactly one secret per connection (see
// internal/connections) — FileMover's Connection struct has independent
// Password and KeyPassphrase fields and can offer both to the server at
// once; supporting a passphrase-protected key WITH a separate fallback
// password too is a rare enough combination that this simplification is an
// acceptable trade for a single keychain entry per connection.
func authMethod(conn *connections.Connection, secret string) (ssh.AuthMethod, error) {
	if conn.SSHKeyPath == "" {
		if secret == "" {
			return nil, errors.New("no password or SSH key configured for this connection")
		}
		return ssh.Password(secret), nil
	}

	keyPath := conn.SSHKeyPath
	if strings.HasPrefix(keyPath, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve SSH key path: %w", err)
		}
		keyPath = filepath.Join(home, strings.TrimPrefix(keyPath, "~"))
	}
	// Mirror the OpenSSH client's own convenience behavior: an existing
	// `ssh -i ~/.ssh/id_rsa.pub ...` alias pointing at the PUBLIC key still
	// works there because ssh(1) uses it only to identify which key pair to
	// offer, then loads the actual private key from the same path minus
	// ".pub". Go's ssh.ParsePrivateKey has no such convenience (it just
	// fails to parse a public key blob as a private one, with a confusing
	// "no key found"), so it's replicated here.
	keyPath = strings.TrimSuffix(keyPath, ".pub")
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read SSH key: %w", err)
	}
	var signer ssh.Signer
	if secret != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(secret))
		// The stored secret may be stale/wrong for a key that in fact has no
		// passphrase at all (e.g. left over from editing this connection, or
		// a keychain entry that differs machine-to-machine) — x/crypto/ssh
		// rejects that combination outright rather than just ignoring the
		// unneeded passphrase, so fall back to the no-passphrase parse
		// instead of failing a connection that would otherwise work fine.
		if err != nil {
			signer, err = ssh.ParsePrivateKey(key)
		}
	} else {
		signer, err = ssh.ParsePrivateKey(key)
	}
	if err != nil {
		return nil, fmt.Errorf("parse SSH key: %w", err)
	}
	return ssh.PublicKeys(signer), nil
}

// Connect dials conn.Host:conn.Port and authenticates as conn.Username.
// Verifies the host key against conn.TrustedHostKeyFingerprint EXACTLY —
// no first-use wildcard acceptance here, that's ProbeHostKey's job, called
// separately by the UI layer before this, with the user's trust decision
// already persisted to conn in between. secret is conn's one stored
// credential (see authMethod).
func Connect(conn *connections.Connection, secret string) (*FS, error) {
	auth, err := authMethod(conn, secret)
	if err != nil {
		return nil, err
	}
	if conn.TrustedHostKeyFingerprint == "" {
		return nil, errors.New("no trusted host key on file for this connection yet")
	}

	config := &ssh.ClientConfig{
		User:    conn.Username,
		Auth:    []ssh.AuthMethod{auth},
		Timeout: 10 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if Fingerprint(key) != conn.TrustedHostKeyFingerprint {
				return errors.New("host key fingerprint does not match the trusted one on file")
			}
			return nil
		},
	}

	a := addr(conn.Host, conn.Port)
	sshConn, err := ssh.Dial("tcp", a, config)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", a, err)
	}
	sftpConn, err := sftp.NewClient(sshConn)
	if err != nil {
		sshConn.Close()
		return nil, fmt.Errorf("start SFTP session: %w", err)
	}
	return &FS{
		sshConn:  sshConn,
		sftpConn: sftpConn,
		prefix:   fmt.Sprintf("sftp://%s@%s", conn.Username, a),
	}, nil
}

// Presented returns remotePath (this connection's own starting path, or any
// path reached by navigating within it) in this FS's presented form.
func (fs *FS) Presented(remotePath string) string {
	if remotePath == "" {
		remotePath = "/"
	}
	if !strings.HasPrefix(remotePath, "/") {
		remotePath = "/" + remotePath
	}
	return fs.prefix + remotePath
}

func (fs *FS) internalPath(presented string) string {
	p := strings.TrimPrefix(presented, fs.prefix)
	if p == "" {
		return "/"
	}
	return p
}

func entryFromInfo(info os.FileInfo) vfs.Entry {
	return vfs.Entry{
		Name:     info.Name(),
		Size:     info.Size(),
		ModTime:  info.ModTime(),
		IsDir:    info.IsDir(),
		Mode:     info.Mode(),
		ReadOnly: info.Mode().Perm()&0o200 == 0,
	}
}

func (fs *FS) ReadDir(p string) ([]vfs.Entry, error) {
	infos, err := fs.sftpConn.ReadDir(fs.internalPath(p))
	if err != nil {
		return nil, err
	}
	entries := make([]vfs.Entry, 0, len(infos))
	for _, info := range infos {
		entries = append(entries, entryFromInfo(info))
	}
	return entries, nil
}

func (fs *FS) Stat(p string) (vfs.Entry, error) {
	info, err := fs.sftpConn.Stat(fs.internalPath(p))
	if err != nil {
		return vfs.Entry{}, err
	}
	return entryFromInfo(info), nil
}

func (fs *FS) Open(p string) (io.ReadCloser, error) {
	return fs.sftpConn.Open(fs.internalPath(p))
}

func (fs *FS) Create(p string) (io.WriteCloser, error) {
	return fs.sftpConn.Create(fs.internalPath(p))
}

func (fs *FS) Mkdir(p string) error {
	return fs.sftpConn.Mkdir(fs.internalPath(p))
}

// Remove deletes p — a file via Remove, a directory via RemoveDirectory
// (which, like os.Remove locally, only succeeds if it's already empty; this
// app's F8 Delete doesn't recurse-then-remove for remote connections in
// this first pass — see the Connections manager plan's deferred-scope note).
func (fs *FS) Remove(p string) error {
	remote := fs.internalPath(p)
	info, err := fs.sftpConn.Stat(remote)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fs.sftpConn.RemoveDirectory(remote)
	}
	return fs.sftpConn.Remove(remote)
}

// Rename uses plain SFTP rename (not the posix-rename@openssh.com
// extension's RemoveAll-if-exists semantics) — it errors rather than
// silently overwriting an existing destination if the server doesn't
// support the extension, which is the safer default across arbitrary SFTP
// servers even though it makes this slightly stricter than a local
// os.Rename on some platforms.
func (fs *FS) Rename(oldPath, newPath string) error {
	return fs.sftpConn.Rename(fs.internalPath(oldPath), fs.internalPath(newPath))
}

// Join concatenates presented paths — deliberately NOT delegated to
// path.Join on the whole string (see the package doc for why: it would
// collapse the "://" in the scheme prefix). elem[0] must already be one of
// this connection's own presented paths, or Join degrades to a plain
// "path"-package join, the same fallback zipfs/listboxfs use for a path
// that isn't theirs.
func (fs *FS) Join(elem ...string) string {
	if len(elem) == 0 {
		return fs.prefix
	}
	if !strings.HasPrefix(elem[0], fs.prefix) {
		return path.Join(elem...)
	}
	result := elem[0]
	for _, e := range elem[1:] {
		e = strings.Trim(e, "/")
		if e == "" {
			continue
		}
		result = strings.TrimRight(result, "/") + "/" + e
	}
	return result
}

// Dir returns presented's parent. At this connection's true top (the
// remote's own real "/"), there's nowhere further up — the same
// "return yourself, no further parent" convention listboxfs.Dir uses for
// its own root — so navigating ".." naturally stops there rather than
// escaping to anything local; Home is the only way back to local browsing
// (see fileListView.adjustFSForTarget and swappableFS).
func (fs *FS) Dir(presented string) string {
	remote := strings.TrimRight(fs.internalPath(presented), "/")
	if remote == "" {
		return fs.Presented("/")
	}
	idx := strings.LastIndex(remote, "/")
	if idx <= 0 {
		return fs.Presented("/")
	}
	return fs.Presented(remote[:idx])
}

// IsInside reports whether target is still within this connection's own
// host — used by adjustFSForTarget to notice when navigation (typically an
// explicit Home/Favorites jump; casual "..” browsing never leaves, per
// Dir's doc comment above) has left this connection entirely. Satisfies
// swappableFS (filelist.go).
func (fs *FS) IsInside(target string) bool {
	return target == fs.prefix || strings.HasPrefix(target, fs.prefix+"/")
}

// Close ends the SFTP session and the underlying SSH connection. Satisfies
// swappableFS.
func (fs *FS) Close() error {
	if fs.sftpConn != nil {
		fs.sftpConn.Close()
	}
	if fs.sshConn != nil {
		return fs.sshConn.Close()
	}
	return nil
}

func (fs *FS) HomeDir() (string, error) {
	return "", errors.New("no home directory shortcut for a remote connection")
}

func (fs *FS) Roots() ([]string, error) {
	return []string{fs.Presented("/")}, nil
}

func fillDefaults(progress fsops.ProgressFunc, resolve fsops.ConflictFunc) (fsops.ProgressFunc, fsops.ConflictFunc) {
	if progress == nil {
		progress = func(int64, int64, string) {}
	}
	if resolve == nil {
		resolve = func(string) (fsops.ConflictAction, string) { return fsops.ConflictOverwrite, "" }
	}
	return progress, resolve
}

// Download copies each of sources (presented paths on this connection,
// files and/or directories, recursively) into destDir on the REAL LOCAL
// filesystem, preserving each source's base name — the SFTP side of F5
// Copy out of a remote tab, and of F3 View (download to a temp file first,
// then the normal viewer, mirroring how zipfs already handles viewing a
// file inside an open archive). Signature matches fsops.Copy/zipfs.Extract
// so the existing progress-dialog/conflict-resolution UI drives it with
// zero changes.
func (fs *FS) Download(sources []string, destDir string, progress fsops.ProgressFunc, resolve fsops.ConflictFunc) error {
	progress, resolve = fillDefaults(progress, resolve)

	remotes := make([]string, 0, len(sources))
	var total int64
	for _, src := range sources {
		remote := fs.internalPath(src)
		remotes = append(remotes, remote)
		size, err := fs.remoteTotalSize(remote)
		if err != nil {
			return err
		}
		total += size
	}

	var done int64
	for _, remote := range remotes {
		dest := filepath.Join(destDir, path.Base(remote))
		if err := fs.downloadOne(remote, dest, &done, total, progress, resolve); err != nil {
			return err
		}
	}
	return nil
}

func (fs *FS) remoteTotalSize(remote string) (int64, error) {
	info, err := fs.sftpConn.Stat(remote)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return info.Size(), nil
	}
	entries, err := fs.sftpConn.ReadDir(remote)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, e := range entries {
		size, err := fs.remoteTotalSize(path.Join(remote, e.Name()))
		if err != nil {
			return 0, err
		}
		total += size
	}
	return total, nil
}

func (fs *FS) downloadOne(remote, dest string, done *int64, total int64, progress fsops.ProgressFunc, resolve fsops.ConflictFunc) error {
	info, err := fs.sftpConn.Stat(remote)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return err
		}
		entries, err := fs.sftpConn.ReadDir(remote)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := fs.downloadOne(path.Join(remote, e.Name()), filepath.Join(dest, e.Name()), done, total, progress, resolve); err != nil {
				return err
			}
		}
		return nil
	}

	if _, err := os.Lstat(dest); err == nil {
		action, newName := resolve(dest)
		switch action {
		case fsops.ConflictSkip:
			*done += info.Size()
			progress(*done, total, dest)
			return nil
		case fsops.ConflictCancel:
			return fsops.ErrCancelled
		case fsops.ConflictRename:
			dest = filepath.Join(filepath.Dir(dest), newName)
		case fsops.ConflictOverwrite:
			// proceed; os.Create below truncates it.
		}
	}

	in, err := fs.sftpConn.Open(remote)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	buf := make([]byte, 256*1024)
	for {
		nr, rerr := in.Read(buf)
		if nr > 0 {
			if _, werr := out.Write(buf[:nr]); werr != nil {
				return werr
			}
			*done += int64(nr)
			progress(*done, total, dest)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	return out.Close()
}

// Upload copies each of sources (REAL LOCAL paths, files and/or
// directories, recursively) into destDir — a PRESENTED path on this
// connection, unlike Download's destDir which is real-local — preserving
// each source's base name. The SFTP side of F5 Copy into a remote tab.
func (fs *FS) Upload(sources []string, destDir string, progress fsops.ProgressFunc, resolve fsops.ConflictFunc) error {
	progress, resolve = fillDefaults(progress, resolve)

	var total int64
	for _, src := range sources {
		size, err := localTotalSize(src)
		if err != nil {
			return err
		}
		total += size
	}

	remoteDestDir := fs.internalPath(destDir)
	var done int64
	for _, src := range sources {
		remoteDest := path.Join(remoteDestDir, filepath.Base(src))
		if err := fs.uploadOne(src, remoteDest, &done, total, progress, resolve); err != nil {
			return err
		}
	}
	return nil
}

func localTotalSize(src string) (int64, error) {
	info, err := os.Lstat(src)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return info.Size(), nil
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, e := range entries {
		size, err := localTotalSize(filepath.Join(src, e.Name()))
		if err != nil {
			return 0, err
		}
		total += size
	}
	return total, nil
}

func (fs *FS) uploadOne(src, remoteDest string, done *int64, total int64, progress fsops.ProgressFunc, resolve fsops.ConflictFunc) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := fs.sftpConn.MkdirAll(remoteDest); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := fs.uploadOne(filepath.Join(src, e.Name()), path.Join(remoteDest, e.Name()), done, total, progress, resolve); err != nil {
				return err
			}
		}
		return nil
	}

	if _, err := fs.sftpConn.Lstat(remoteDest); err == nil {
		action, newName := resolve(fs.Presented(remoteDest))
		switch action {
		case fsops.ConflictSkip:
			*done += info.Size()
			progress(*done, total, remoteDest)
			return nil
		case fsops.ConflictCancel:
			return fsops.ErrCancelled
		case fsops.ConflictRename:
			remoteDest = path.Join(path.Dir(remoteDest), newName)
		case fsops.ConflictOverwrite:
			// proceed; sftpConn.Create below truncates it.
		}
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := fs.sftpConn.Create(remoteDest)
	if err != nil {
		return err
	}
	defer out.Close()

	buf := make([]byte, 256*1024)
	for {
		nr, rerr := in.Read(buf)
		if nr > 0 {
			if _, werr := out.Write(buf[:nr]); werr != nil {
				return werr
			}
			*done += int64(nr)
			progress(*done, total, remoteDest)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	return out.Close()
}
