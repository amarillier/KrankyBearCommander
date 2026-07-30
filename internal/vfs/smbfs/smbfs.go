// Package smbfs is an SMB-backed vfs.FileSystem, so a pane can browse a
// share exactly like a real directory (see the Connections manager,
// connections_ui.go) — the same shape internal/vfs/sftpfs already
// established for SFTP (a "presented path" scheme prefix, Download/Upload
// transfer methods), adapted for SMB's own auth (NTLM: username/password,
// optional domain — no host-key concept the way SSH has one) and its one
// real structural difference: a connection's RemotePath must first name
// which SHARE to mount, with any path within it as an optional suffix (see
// parseShareAndPath).
//
// Paths this package hands out and accepts ("presented" paths) are
// "smb://user@host:port/ShareName" followed by the share's own
// "/"-separated path — e.g. "smb://allan@192.168.1.50:445/Users" (a
// share's own top) or ".../Users/allan/docs". go-smb2's own Share methods
// require "\"-separated, non-leading-slash paths (its path.go rejects a
// leading separator outright) — see shareRelative, the one place that
// conversion happens; everywhere else in this package works with the same
// "/"-based presented-path convention every other backend uses.
package smbfs

import (
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/hirochachacha/go-smb2"

	"commander/internal/connections"
	"commander/internal/fsops"
	"commander/internal/vfs"
)

// FS is one open SMB session, mounted on a single share.
type FS struct {
	netConn net.Conn
	session *smb2.Session
	share   *smb2.Share
	prefix  string // "smb://user@host:port/ShareName" — no trailing slash
	// startPath is the within-share path parsed from the Connection's
	// RemotePath at Connect time — StartPath() turns it into this
	// connection's own starting presented path.
	startPath string
}

func addr(host string, port int) string {
	if port == 0 {
		port = 445
	}
	return fmt.Sprintf("%s:%d", host, port)
}

// parseShareAndPath splits a Connection.RemotePath into the share name to
// mount and the path within it — accepted forms: "ShareName",
// "ShareName/sub/dir", "ShareName\sub\dir", or a UNC-style
// "\\server\ShareName\sub\dir" (the server segment is redundant — the
// connection's own Host is used instead — and simply skipped).
func parseShareAndPath(remotePath string) (share, withinShare string, err error) {
	p := strings.ReplaceAll(strings.TrimSpace(remotePath), "\\", "/")
	if strings.HasPrefix(p, "//") {
		p = strings.TrimPrefix(p, "//")
		if idx := strings.IndexByte(p, '/'); idx >= 0 {
			p = p[idx+1:]
		} else {
			p = ""
		}
	}
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return "", "", fmt.Errorf(`no share name configured — set Remote Path to a share name, e.g. "Users" or "Users/allan"`)
	}
	parts := strings.SplitN(p, "/", 2)
	share = parts[0]
	withinShare = "/"
	if len(parts) == 2 && parts[1] != "" {
		withinShare = "/" + parts[1]
	}
	return share, withinShare, nil
}

// Connect dials host:port, authenticates as conn.Username via NTLM (secret
// is the password; conn.Domain is optional), and mounts the share named by
// conn.RemotePath's first path component.
func Connect(conn *connections.Connection, secret string) (*FS, error) {
	share, withinShare, err := parseShareAndPath(conn.RemotePath)
	if err != nil {
		return nil, err
	}

	a := addr(conn.Host, conn.Port)
	netConn, err := net.Dial("tcp", a)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", a, err)
	}

	dialer := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			User:     conn.Username,
			Password: secret,
			Domain:   conn.Domain,
		},
	}
	session, err := dialer.Dial(netConn)
	if err != nil {
		netConn.Close()
		return nil, fmt.Errorf("SMB session to %s: %w", a, err)
	}

	smbShare, err := session.Mount(share)
	if err != nil {
		session.Logoff()
		netConn.Close()
		return nil, fmt.Errorf("mount share %q: %w", share, err)
	}

	return &FS{
		netConn:   netConn,
		session:   session,
		share:     smbShare,
		prefix:    fmt.Sprintf("smb://%s@%s/%s", conn.Username, a, share),
		startPath: withinShare,
	}, nil
}

// StartPath is this connection's own starting presented path (the share's
// top, or a path within it, per conn.RemotePath at Connect time).
func (fs *FS) StartPath() string {
	return fs.Presented(fs.startPath)
}

// Presented returns withinShare (this connection's own, or any path reached
// by navigating within it) in this FS's presented form.
func (fs *FS) Presented(withinShare string) string {
	if withinShare == "" {
		withinShare = "/"
	}
	if !strings.HasPrefix(withinShare, "/") {
		withinShare = "/" + withinShare
	}
	return fs.prefix + withinShare
}

func (fs *FS) internalPath(presented string) string {
	p := strings.TrimPrefix(presented, fs.prefix)
	if p == "" {
		return "/"
	}
	return p
}

// shareRelative converts a presented path to the form go-smb2's Share
// methods actually accept: no leading separator at all (its own path.go
// rejects one outright, in every method that matters here) — "/" (this
// share's own top) becomes "", "/allan/docs" becomes "allan/docs". Still
// "/"-separated; go-smb2 normalizes to "\" internally on its own.
func (fs *FS) shareRelative(presented string) string {
	return strings.TrimPrefix(fs.internalPath(presented), "/")
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
	infos, err := fs.share.ReadDir(fs.shareRelative(p))
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
	info, err := fs.share.Stat(fs.shareRelative(p))
	if err != nil {
		return vfs.Entry{}, err
	}
	return entryFromInfo(info), nil
}

func (fs *FS) Open(p string) (io.ReadCloser, error) {
	return fs.share.Open(fs.shareRelative(p))
}

func (fs *FS) Create(p string) (io.WriteCloser, error) {
	return fs.share.Create(fs.shareRelative(p))
}

func (fs *FS) Mkdir(p string) error {
	return fs.share.Mkdir(fs.shareRelative(p), 0o755)
}

// Remove deletes p, file or (empty) directory alike — unlike sftpfs (where
// the wire protocol has genuinely separate file/directory delete ops,
// needing a Stat first to pick the right one), go-smb2's own Share.Remove
// already handles both uniformly, the same as a real os.Remove would.
func (fs *FS) Remove(p string) error {
	return fs.share.Remove(fs.shareRelative(p))
}

func (fs *FS) Rename(oldPath, newPath string) error {
	return fs.share.Rename(fs.shareRelative(oldPath), fs.shareRelative(newPath))
}

// Join concatenates presented paths — deliberately NOT delegated to
// path.Join on the whole string (see sftpfs's package doc for why: it
// would collapse the "://" in the scheme prefix). elem[0] must already be
// one of this connection's own presented paths, or Join degrades to a
// plain "path"-package join, the same fallback every other backend uses
// for a path that isn't theirs.
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

// Dir returns presented's parent. At this share's true top, there's
// nowhere further up — the same "return yourself, no further parent"
// convention every other synthetic backend's Dir uses for its own root —
// so ".." naturally stops there rather than escaping to anything local;
// Home is the only way back to local browsing (see
// fileListView.adjustFSForTarget and swappableFS). Navigation can go
// anywhere above the connection's own configured starting path, up to the
// share's real top — RemotePath is only ever a starting point, not a
// boundary, matching sftpfs's own behavior.
func (fs *FS) Dir(presented string) string {
	within := strings.TrimRight(fs.internalPath(presented), "/")
	if within == "" {
		return fs.Presented("/")
	}
	idx := strings.LastIndex(within, "/")
	if idx <= 0 {
		return fs.Presented("/")
	}
	return fs.Presented(within[:idx])
}

// IsInside reports whether target is still within this share — used by
// adjustFSForTarget to notice when navigation (an explicit Home/Favorites
// jump; casual ".." browsing never leaves, per Dir's doc comment above)
// has left this connection entirely. Satisfies swappableFS (filelist.go).
func (fs *FS) IsInside(target string) bool {
	return target == fs.prefix || strings.HasPrefix(target, fs.prefix+"/")
}

// Close unmounts the share, logs off the session, and closes the
// underlying TCP connection. Satisfies swappableFS.
func (fs *FS) Close() error {
	if fs.share != nil {
		fs.share.Umount()
	}
	if fs.session != nil {
		fs.session.Logoff()
	}
	if fs.netConn != nil {
		return fs.netConn.Close()
	}
	return nil
}

func (fs *FS) HomeDir() (string, error) {
	return "", fmt.Errorf("no home directory shortcut for a remote connection")
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
// filesystem, preserving each source's base name — the SMB side of F5 Copy
// out of a remote tab, and of F3 View (download to a temp file first, then
// the normal viewer). Signature matches fsops.Copy/zipfs.Extract/
// sftpfs.Download so the existing progress-dialog/conflict-resolution UI
// drives it with zero changes.
func (fs *FS) Download(sources []string, destDir string, progress fsops.ProgressFunc, resolve fsops.ConflictFunc) error {
	progress, resolve = fillDefaults(progress, resolve)

	names := make([]string, 0, len(sources))
	var total int64
	for _, src := range sources {
		name := fs.shareRelative(src)
		names = append(names, name)
		size, err := fs.shareTotalSize(name)
		if err != nil {
			return err
		}
		total += size
	}

	var done int64
	for _, name := range names {
		dest := filepath.Join(destDir, path.Base(name))
		if err := fs.downloadOne(name, dest, &done, total, progress, resolve); err != nil {
			return err
		}
	}
	return nil
}

func (fs *FS) shareTotalSize(name string) (int64, error) {
	info, err := fs.share.Stat(name)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return info.Size(), nil
	}
	entries, err := fs.share.ReadDir(name)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, e := range entries {
		size, err := fs.shareTotalSize(path.Join(name, e.Name()))
		if err != nil {
			return 0, err
		}
		total += size
	}
	return total, nil
}

func (fs *FS) downloadOne(name, dest string, done *int64, total int64, progress fsops.ProgressFunc, resolve fsops.ConflictFunc) error {
	info, err := fs.share.Stat(name)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return err
		}
		entries, err := fs.share.ReadDir(name)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := fs.downloadOne(path.Join(name, e.Name()), filepath.Join(dest, e.Name()), done, total, progress, resolve); err != nil {
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

	in, err := fs.share.Open(name)
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
// each source's base name. The SMB side of F5 Copy into a remote tab.
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

	destName := fs.shareRelative(destDir)
	var done int64
	for _, src := range sources {
		name := path.Join(destName, filepath.Base(src))
		if err := fs.uploadOne(src, name, &done, total, progress, resolve); err != nil {
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

func (fs *FS) uploadOne(src, name string, done *int64, total int64, progress fsops.ProgressFunc, resolve fsops.ConflictFunc) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := fs.share.MkdirAll(name, 0o755); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := fs.uploadOne(filepath.Join(src, e.Name()), path.Join(name, e.Name()), done, total, progress, resolve); err != nil {
				return err
			}
		}
		return nil
	}

	if _, err := fs.share.Lstat(name); err == nil {
		action, newName := resolve(fs.Presented(name))
		switch action {
		case fsops.ConflictSkip:
			*done += info.Size()
			progress(*done, total, name)
			return nil
		case fsops.ConflictCancel:
			return fsops.ErrCancelled
		case fsops.ConflictRename:
			name = path.Join(path.Dir(name), newName)
		case fsops.ConflictOverwrite:
			// proceed; share.Create below truncates it.
		}
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := fs.share.Create(name)
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
			progress(*done, total, name)
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
