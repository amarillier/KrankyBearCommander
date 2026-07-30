// Package fileagentfs is a vfs.FileSystem client for KrankyBearFileMover's
// own "FileAgent" protocol (TLS 1.3 + a pinned certificate fingerprint +
// pre-shared key, JSON-over-length-prefixed-TCP — see protocol.go, ported
// byte-for-byte from FileMover's fileagent_protocol.go/fileagent_client.go
// so this client talks to a real `-file-agent` listener unmodified), so a
// pane can browse a shared folder exactly like a real directory (see the
// Connections manager, connections_ui.go) — the same shape
// internal/vfs/sftpfs and internal/vfs/smbfs already established.
//
// Unlike SFTP's host-key trust-on-first-use, there's no probe-then-prompt
// step here: the TLS certificate pin is expected to already be known out
// of band (the user copies it from wherever the listener printed it on
// startup) before the first Connect. Auth has no username at all, just a
// pre-shared key — see Connect.
//
// One deliberate departure from FileMover's own client, with NO wire-format
// difference: FileMover's ReadFile hands its connection mutex to the
// returned io.ReadCloser, only released in its Close() — a real deadlock
// risk if a caller ever forgets to call it. This package instead does every
// read/write fully inside one locked method (buffering a whole file for
// Open/Create, or streaming straight to/from a local file for
// Download/Upload), so the mutex is always released via a plain defer,
// never handed across a call boundary.
//
// Paths this package hands out and accepts ("presented" paths) are
// "fileagent://host:port" followed by the agent's own "/"-rooted virtual
// path — e.g. "fileagent://192.168.1.50:9742" (this connection's own top)
// or ".../docs/report.txt". Because that scheme prefix contains "//"
// (which path.Clean/filepath.Clean would collapse if run over the WHOLE
// string), Join/Dir below deliberately never hand the full presented
// string to the "path" package — only ever the bare virtual path, once the
// prefix has been stripped off — the same convention sftpfs/smbfs already
// established.
package fileagentfs

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"commander/internal/connections"
	"commander/internal/fsops"
	"commander/internal/vfs"
)

// FS is one open FileAgent connection.
type FS struct {
	mu     sync.Mutex
	conn   *tls.Conn
	prefix string // "fileagent://host:port" — no trailing slash
}

func normalizePin(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, ":", "")
	s = strings.ReplaceAll(s, " ", "")
	return s
}

func addr(host string, port int) string {
	if port == 0 {
		port = defaultPort
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// normalizeVirtualPath ensures a leading "/" and runs path.Clean, mapping
// an empty result back to "/" — simpler than FileMover's own
// fileAgentNormalizeVirtualPath (which also maps typed Windows drive-root
// aliases like "C:\" to "/", a convenience for FileMover's own hand-typed
// path bar that doesn't apply here, since Commander builds paths via
// Join/Dir rather than parsing free-typed input).
func normalizeVirtualPath(p string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	p = path.Clean(p)
	if p == "" || p == "." {
		return "/"
	}
	return p
}

// Connect dials host:port, verifies the peer's TLS certificate against
// conn.FileAgentTLSPin, and authenticates with secret as the pre-shared
// key — ported from FileMover's FileAgentConnection.dial.
func Connect(conn *connections.Connection, secret string) (*FS, error) {
	pin := normalizePin(conn.FileAgentTLSPin)
	if pin == "" {
		return nil, errors.New("a TLS certificate pin is required for a FileAgent connection")
	}
	expected, err := hex.DecodeString(pin)
	if err != nil || len(expected) != sha256.Size {
		return nil, fmt.Errorf("TLS pin must be %d hex characters (SHA-256)", sha256.Size*2)
	}

	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("no certificate from server")
			}
			sum := sha256.Sum256(rawCerts[0])
			if subtle.ConstantTimeCompare(sum[:], expected) != 1 {
				// Spell out both sides — configured vs. what the server just
				// presented — so a mismatch is immediately diagnosable (e.g.
				// confirms whether this connection is really still using an
				// old/stale pin) instead of a bare "doesn't match".
				return fmt.Errorf("TLS certificate fingerprint does not match the saved pin — this connection is configured with %s, the server just presented %s", pin, hex.EncodeToString(sum[:]))
			}
			return nil
		},
	}

	a := addr(conn.Host, conn.Port)
	d := net.Dialer{Timeout: dialTimeout}
	tcpConn, err := d.Dial("tcp", a)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", a, err)
	}
	tlsConn := tls.Client(tcpConn, tlsCfg)
	if err := tlsConn.Handshake(); err != nil {
		tcpConn.Close()
		return nil, fmt.Errorf("TLS handshake with %s: %w", a, err)
	}

	if err := writeJSONFrame(tlsConn, msg{Op: opAuth, V: protocolVer, PSK: secret}); err != nil {
		tlsConn.Close()
		return nil, err
	}
	var resp msg
	if err := readJSONFrame(tlsConn, &resp); err != nil {
		tlsConn.Close()
		return nil, err
	}
	switch resp.Op {
	case opAuthOK:
	case opAuthFail:
		tlsConn.Close()
		return nil, fmt.Errorf("authentication failed: %s", resp.Msg)
	default:
		tlsConn.Close()
		return nil, fmt.Errorf("unexpected auth response: %s", resp.Op)
	}

	return &FS{conn: tlsConn, prefix: "fileagent://" + a}, nil
}

// Presented returns virtual (this connection's own starting path, or any
// path reached by navigating within it) in this FS's presented form.
func (fs *FS) Presented(virtual string) string {
	return fs.prefix + normalizeVirtualPath(virtual)
}

func (fs *FS) internalPath(presented string) string {
	p := strings.TrimPrefix(presented, fs.prefix)
	if p == "" {
		return "/"
	}
	return p
}

// list sends one "list" request and parses its "listResp" — the only
// listing primitive the wire protocol has; Stat below derives a single
// entry's info from it, same as the protocol offers no dedicated stat op.
func (fs *FS) list(virtual string) ([]vfs.Entry, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if err := writeJSONFrame(fs.conn, msg{Op: opList, Path: virtual}); err != nil {
		return nil, err
	}
	var resp msg
	if err := readJSONFrame(fs.conn, &resp); err != nil {
		return nil, err
	}
	switch resp.Op {
	case opError:
		return nil, errors.New(resp.Msg)
	case opListResp:
	default:
		return nil, fmt.Errorf("unexpected list response: %s", resp.Op)
	}
	entries := make([]vfs.Entry, 0, len(resp.Entries))
	for _, e := range resp.Entries {
		mode := os.FileMode(e.Mode)
		entries = append(entries, vfs.Entry{
			Name:     e.Name,
			Size:     e.Size,
			ModTime:  time.Unix(e.ModUnix, 0),
			IsDir:    e.IsDir,
			Mode:     mode,
			ReadOnly: mode.Perm()&0o200 == 0,
		})
	}
	return entries, nil
}

func (fs *FS) statVirtual(virtual string) (vfs.Entry, error) {
	if virtual == "/" || virtual == "" {
		return vfs.Entry{Name: "/", IsDir: true}, nil
	}
	entries, err := fs.list(path.Dir(virtual))
	if err != nil {
		return vfs.Entry{}, err
	}
	name := path.Base(virtual)
	for _, e := range entries {
		if e.Name == name {
			return e, nil
		}
	}
	return vfs.Entry{}, fmt.Errorf("%s: not found", virtual)
}

func (fs *FS) ReadDir(p string) ([]vfs.Entry, error) {
	return fs.list(fs.internalPath(p))
}

func (fs *FS) Stat(p string) (vfs.Entry, error) {
	return fs.statVirtual(fs.internalPath(p))
}

// readAgent fetches virtual's whole content into memory — see the package
// doc for why this (rather than FileMover's own lock-handoff streaming
// reader) is this package's Open(), with Download below streaming straight
// to a local file instead for the actual bulk-transfer path.
func (fs *FS) readAgent(virtual string) ([]byte, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if err := writeJSONFrame(fs.conn, msg{Op: opRead, Path: virtual}); err != nil {
		return nil, err
	}
	var head msg
	if err := readJSONFrame(fs.conn, &head); err != nil {
		return nil, err
	}
	switch head.Op {
	case opError:
		return nil, errors.New(head.Msg)
	case opReadHdr:
	default:
		return nil, fmt.Errorf("unexpected read response: %s", head.Op)
	}
	data := make([]byte, head.Size)
	if head.Size > 0 {
		if _, err := io.ReadFull(fs.conn, data); err != nil {
			return nil, err
		}
	}
	var done msg
	if err := readJSONFrame(fs.conn, &done); err != nil {
		return nil, err
	}
	if done.Op == opError {
		return nil, errors.New(done.Msg)
	}
	if done.Op != opReadDone {
		return nil, fmt.Errorf("unexpected read trailer: %s", done.Op)
	}
	return data, nil
}

func (fs *FS) Open(p string) (io.ReadCloser, error) {
	data, err := fs.readAgent(fs.internalPath(p))
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// writeAgent uploads all of r's content to virtual, streaming in maxChunk
// pieces — the writeBeg/writeReady/(writeChk+raw bytes)*/writeEnd/writeOK
// sequence, done/progress updated per chunk actually sent.
func (fs *FS) writeAgent(virtual string, r io.Reader, done *int64, total int64, progress fsops.ProgressFunc) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if err := writeJSONFrame(fs.conn, msg{Op: opWriteBeg, Path: virtual}); err != nil {
		return err
	}
	var ack msg
	if err := readJSONFrame(fs.conn, &ack); err != nil {
		return err
	}
	switch ack.Op {
	case opError:
		return errors.New(ack.Msg)
	case opWriteReady:
	default:
		return fmt.Errorf("unexpected write ack: %s", ack.Op)
	}

	buf := make([]byte, maxChunk)
	for {
		n, rerr := r.Read(buf)
		if n > 0 {
			if err := writeJSONFrame(fs.conn, msg{Op: opWriteChk, N: n}); err != nil {
				return err
			}
			if _, err := fs.conn.Write(buf[:n]); err != nil {
				return err
			}
			*done += int64(n)
			progress(*done, total, virtual)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}

	if err := writeJSONFrame(fs.conn, msg{Op: opWriteEnd}); err != nil {
		return err
	}
	var resp msg
	if err := readJSONFrame(fs.conn, &resp); err != nil {
		return err
	}
	if resp.Op == opError {
		return errors.New(resp.Msg)
	}
	if resp.Op != opWriteOK {
		return fmt.Errorf("unexpected write response: %s", resp.Op)
	}
	return nil
}

// uploadBuffer buffers Write calls in memory and performs the actual
// writeAgent upload only on Close — the protocol needs a read-until-EOF
// source, which a generic io.WriteCloser's incremental Write calls can't
// supply until the caller signals "done" by closing.
type uploadBuffer struct {
	fs      *FS
	virtual string
	buf     bytes.Buffer
}

func (u *uploadBuffer) Write(p []byte) (int, error) { return u.buf.Write(p) }
func (u *uploadBuffer) Close() error {
	var done int64
	return u.fs.writeAgent(u.virtual, &u.buf, &done, int64(u.buf.Len()), func(int64, int64, string) {})
}

func (fs *FS) Create(p string) (io.WriteCloser, error) {
	return &uploadBuffer{fs: fs, virtual: fs.internalPath(p)}, nil
}

func (fs *FS) simpleOp(m msg) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if err := writeJSONFrame(fs.conn, m); err != nil {
		return err
	}
	var resp msg
	if err := readJSONFrame(fs.conn, &resp); err != nil {
		return err
	}
	if resp.Op == opError {
		return errors.New(resp.Msg)
	}
	if resp.Op != opOK {
		return fmt.Errorf("unexpected response: %s", resp.Op)
	}
	return nil
}

func (fs *FS) Mkdir(p string) error {
	return fs.simpleOp(msg{Op: opMkdir, Path: fs.internalPath(p)})
}

func (fs *FS) Remove(p string) error {
	return fs.simpleOp(msg{Op: opRemove, Path: fs.internalPath(p)})
}

func (fs *FS) Rename(oldPath, newPath string) error {
	return fs.simpleOp(msg{Op: opRename, Path: fs.internalPath(oldPath), NewPath: fs.internalPath(newPath)})
}

// Join concatenates presented paths — deliberately NOT delegated to
// path.Join on the whole string (see the package doc for why). elem[0]
// must already be one of this connection's own presented paths, or Join
// degrades to a plain "path"-package join, the same fallback
// sftpfs/smbfs use for a path that isn't theirs.
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

// Dir returns presented's parent. At this connection's true top, there's
// nowhere further up — the same "return yourself, no further parent"
// convention every other synthetic backend's Dir uses for its own root —
// so ".." naturally stops there rather than escaping to anything local;
// Home is the only way back to local browsing (see
// fileListView.adjustFSForTarget and swappableFS).
func (fs *FS) Dir(presented string) string {
	virtual := strings.TrimRight(fs.internalPath(presented), "/")
	if virtual == "" {
		return fs.Presented("/")
	}
	idx := strings.LastIndex(virtual, "/")
	if idx <= 0 {
		return fs.Presented("/")
	}
	return fs.Presented(virtual[:idx])
}

// IsInside reports whether target is still within this connection — used
// by adjustFSForTarget to notice when navigation has left it entirely.
// Satisfies swappableFS (filelist.go).
func (fs *FS) IsInside(target string) bool {
	return target == fs.prefix || strings.HasPrefix(target, fs.prefix+"/")
}

// Close ends the TLS connection. Satisfies swappableFS.
func (fs *FS) Close() error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.conn == nil {
		return nil
	}
	err := fs.conn.Close()
	fs.conn = nil
	return err
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
// filesystem, preserving each source's base name — the FileAgent side of
// F5 Copy out of a remote tab, and of F3 View (download to a temp file
// first, then the normal viewer). Signature matches
// fsops.Copy/zipfs.Extract/sftpfs.Download/smbfs.Download so the existing
// progress-dialog/conflict-resolution UI drives it with zero changes.
func (fs *FS) Download(sources []string, destDir string, progress fsops.ProgressFunc, resolve fsops.ConflictFunc) error {
	progress, resolve = fillDefaults(progress, resolve)

	virtuals := make([]string, 0, len(sources))
	var total int64
	for _, src := range sources {
		v := fs.internalPath(src)
		virtuals = append(virtuals, v)
		size, err := fs.totalSize(v)
		if err != nil {
			return err
		}
		total += size
	}

	var done int64
	for _, v := range virtuals {
		dest := filepath.Join(destDir, path.Base(v))
		if err := fs.downloadRecursive(v, dest, &done, total, progress, resolve); err != nil {
			return err
		}
	}
	return nil
}

func (fs *FS) totalSize(virtual string) (int64, error) {
	entry, err := fs.statVirtual(virtual)
	if err != nil {
		return 0, err
	}
	if !entry.IsDir {
		return entry.Size, nil
	}
	entries, err := fs.list(virtual)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, e := range entries {
		size, err := fs.totalSize(path.Join(virtual, e.Name))
		if err != nil {
			return 0, err
		}
		total += size
	}
	return total, nil
}

func (fs *FS) downloadRecursive(virtual, dest string, done *int64, total int64, progress fsops.ProgressFunc, resolve fsops.ConflictFunc) error {
	entry, err := fs.statVirtual(virtual)
	if err != nil {
		return err
	}
	if entry.IsDir {
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return err
		}
		entries, err := fs.list(virtual)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := fs.downloadRecursive(path.Join(virtual, e.Name), filepath.Join(dest, e.Name), done, total, progress, resolve); err != nil {
				return err
			}
		}
		return nil
	}

	if _, err := os.Lstat(dest); err == nil {
		action, newName := resolve(dest)
		switch action {
		case fsops.ConflictSkip:
			*done += entry.Size
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
	return fs.downloadOneFile(virtual, dest, done, total, progress)
}

// downloadOneFile streams virtual's content directly to a local dest file,
// chunk by chunk — the read/readHdr/<raw bytes>/readDone sequence, all
// within one held lock (see the package doc for why this differs from
// FileMover's own client without changing anything on the wire).
func (fs *FS) downloadOneFile(virtual, dest string, done *int64, total int64, progress fsops.ProgressFunc) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if err := writeJSONFrame(fs.conn, msg{Op: opRead, Path: virtual}); err != nil {
		return err
	}
	var head msg
	if err := readJSONFrame(fs.conn, &head); err != nil {
		return err
	}
	switch head.Op {
	case opError:
		return errors.New(head.Msg)
	case opReadHdr:
	default:
		return fmt.Errorf("unexpected read response: %s", head.Op)
	}

	out, createErr := os.Create(dest)
	remain := head.Size
	buf := make([]byte, maxChunk)
	for remain > 0 {
		want := int64(len(buf))
		if want > remain {
			want = remain
		}
		n, err := io.ReadFull(fs.conn, buf[:want])
		if n > 0 {
			if createErr == nil {
				if _, werr := out.Write(buf[:n]); werr != nil && createErr == nil {
					createErr = werr
				}
			}
			remain -= int64(n)
			*done += int64(n)
			progress(*done, total, dest)
		}
		if err != nil {
			// Still drain the rest so the trailer frame lines up correctly.
			_, _ = io.CopyN(io.Discard, fs.conn, remain)
			var trailer msg
			_ = readJSONFrame(fs.conn, &trailer)
			if out != nil {
				out.Close()
			}
			return err
		}
	}

	var trailer msg
	if err := readJSONFrame(fs.conn, &trailer); err != nil {
		if out != nil {
			out.Close()
		}
		return err
	}
	if out != nil {
		if cerr := out.Close(); cerr != nil && createErr == nil {
			createErr = cerr
		}
	}
	if trailer.Op == opError {
		return errors.New(trailer.Msg)
	}
	if trailer.Op != opReadDone {
		return fmt.Errorf("unexpected read trailer: %s", trailer.Op)
	}
	return createErr
}

// Upload copies each of sources (REAL LOCAL paths, files and/or
// directories, recursively) into destDir — a PRESENTED path on this
// connection, unlike Download's destDir which is real-local — preserving
// each source's base name. The FileAgent side of F5 Copy into a remote tab.
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

	destVirtual := fs.internalPath(destDir)
	var done int64
	for _, src := range sources {
		virtual := path.Join(destVirtual, filepath.Base(src))
		if err := fs.uploadRecursive(src, virtual, &done, total, progress, resolve); err != nil {
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

func (fs *FS) uploadRecursive(src, virtual string, done *int64, total int64, progress fsops.ProgressFunc, resolve fsops.ConflictFunc) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		// Best-effort: the protocol reports mkdir failures as a free-text
		// Msg with no error code, so "already exists" can't be reliably
		// distinguished from a real problem here — a genuine failure
		// (permissions, disk full) will surface clearly on the file writes
		// that follow anyway.
		_ = fs.Mkdir(fs.Presented(virtual))
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := fs.uploadRecursive(filepath.Join(src, e.Name()), path.Join(virtual, e.Name()), done, total, progress, resolve); err != nil {
				return err
			}
		}
		return nil
	}

	if _, err := fs.statVirtual(virtual); err == nil {
		action, newName := resolve(fs.Presented(virtual))
		switch action {
		case fsops.ConflictSkip:
			*done += info.Size()
			progress(*done, total, virtual)
			return nil
		case fsops.ConflictCancel:
			return fsops.ErrCancelled
		case fsops.ConflictRename:
			virtual = path.Join(path.Dir(virtual), newName)
		case fsops.ConflictOverwrite:
			// proceed; the server overwrites on writeEnd.
		}
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return fs.writeAgent(virtual, in, done, total, progress)
}
