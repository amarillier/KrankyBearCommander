package fileagentfs

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testServer is a minimal, protocol-faithful FileAgent listener backed by a
// real temp directory — enough of the server side (auth, list, read,
// write, mkdir, remove, rename) to exercise this package's client against
// genuine wire traffic, without needing a real FileMover instance.
type testServer struct {
	root string
	psk  string
	pin  string
	ln   net.Listener
}

func generateTestCert(t *testing.T) tls.Certificate {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "fileagentfs-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}
}

func startTestServer(t *testing.T, root, psk string) *testServer {
	t.Helper()
	cert := generateTestCert(t)
	sum := sha256.Sum256(cert.Certificate[0])
	pin := hex.EncodeToString(sum[:])

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	})
	if err != nil {
		t.Fatal(err)
	}
	s := &testServer{root: root, psk: psk, pin: pin, ln: ln}
	go s.serve()
	return s
}

func (s *testServer) addr() string { return s.ln.Addr().String() }
func (s *testServer) close()       { s.ln.Close() }

func (s *testServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *testServer) real(virtual string) string {
	return filepath.Join(s.root, filepath.FromSlash(strings.TrimPrefix(virtual, "/")))
}

func (s *testServer) handleConn(conn net.Conn) {
	defer conn.Close()

	var auth msg
	if err := readJSONFrame(conn, &auth); err != nil {
		return
	}
	if auth.Op != opAuth || auth.PSK != s.psk {
		_ = writeJSONFrame(conn, msg{Op: opAuthFail, Msg: "bad psk"})
		return
	}
	if err := writeJSONFrame(conn, msg{Op: opAuthOK}); err != nil {
		return
	}

	for {
		var m msg
		if err := readJSONFrame(conn, &m); err != nil {
			return
		}
		switch m.Op {
		case opList:
			s.handleList(conn, m)
		case opRead:
			s.handleRead(conn, m)
		case opWriteBeg:
			s.handleWrite(conn, m)
		case opMkdir:
			s.respondOK(conn, os.MkdirAll(s.real(m.Path), 0o755))
		case opRemove:
			s.respondOK(conn, os.Remove(s.real(m.Path)))
		case opRename:
			s.respondOK(conn, os.Rename(s.real(m.Path), s.real(m.NewPath)))
		default:
			_ = writeJSONFrame(conn, msg{Op: opError, Msg: "unknown op"})
		}
	}
}

func (s *testServer) respondOK(conn net.Conn, err error) {
	if err != nil {
		_ = writeJSONFrame(conn, msg{Op: opError, Msg: err.Error()})
		return
	}
	_ = writeJSONFrame(conn, msg{Op: opOK})
}

func (s *testServer) handleList(conn net.Conn, m msg) {
	entries, err := os.ReadDir(s.real(m.Path))
	if err != nil {
		_ = writeJSONFrame(conn, msg{Op: opError, Msg: err.Error()})
		return
	}
	out := make([]wireEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, wireEntry{
			Name:    e.Name(),
			Size:    info.Size(),
			ModUnix: info.ModTime().Unix(),
			IsDir:   info.IsDir(),
			Mode:    uint32(info.Mode()),
		})
	}
	_ = writeJSONFrame(conn, msg{Op: opListResp, Entries: out})
}

func (s *testServer) handleRead(conn net.Conn, m msg) {
	data, err := os.ReadFile(s.real(m.Path))
	if err != nil {
		_ = writeJSONFrame(conn, msg{Op: opError, Msg: err.Error()})
		return
	}
	if err := writeJSONFrame(conn, msg{Op: opReadHdr, Size: int64(len(data))}); err != nil {
		return
	}
	if len(data) > 0 {
		if _, err := conn.Write(data); err != nil {
			return
		}
	}
	_ = writeJSONFrame(conn, msg{Op: opReadDone})
}

func (s *testServer) handleWrite(conn net.Conn, m msg) {
	if err := writeJSONFrame(conn, msg{Op: opWriteReady}); err != nil {
		return
	}
	var buf bytes.Buffer
	for {
		var chunk msg
		if err := readJSONFrame(conn, &chunk); err != nil {
			return
		}
		if chunk.Op == opWriteEnd {
			break
		}
		if chunk.Op != opWriteChk {
			_ = writeJSONFrame(conn, msg{Op: opError, Msg: "expected writeChk"})
			return
		}
		data := make([]byte, chunk.N)
		if _, err := io.ReadFull(conn, data); err != nil {
			return
		}
		buf.Write(data)
	}
	if err := os.WriteFile(s.real(m.Path), buf.Bytes(), 0o644); err != nil {
		_ = writeJSONFrame(conn, msg{Op: opError, Msg: err.Error()})
		return
	}
	_ = writeJSONFrame(conn, msg{Op: opWriteOK})
}
