package fileagentfs

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Wire protocol ported from KrankyBearFileMover's fileagent_protocol.go —
// byte-for-byte compatible with a real FileMover instance running
// `-file-agent`, so this client can talk to it (or to another Commander
// instance, should one ever run a listener) unmodified. Every frame is a
// 4-byte big-endian length prefix followed by exactly that many payload
// bytes — no magic/version byte, no checksum. Raw file-content bytes
// (read/write payload chunks) are NOT framed this way at all: they're
// written/read directly on the connection, sized by the Size (read) or N
// (write) field of the JSON control message that announces them.
const (
	maxFrame    = 64 << 20 // 64 MiB
	maxChunk    = 256 * 1024
	protocolVer = 1
	defaultPort = 9742
	dialTimeout = 45 * time.Second
)

const (
	opAuth       = "auth"
	opAuthOK     = "authOK"
	opAuthFail   = "authFail"
	opError      = "error"
	opList       = "list"
	opListResp   = "listResp"
	opRead       = "read"
	opReadHdr    = "readHdr"
	opReadDone   = "readDone"
	opWriteBeg   = "writeBeg"
	opWriteReady = "writeReady"
	opWriteChk   = "writeChk"
	opWriteEnd   = "writeEnd"
	opWriteOK    = "writeOK"
	opMkdir      = "mkdir"
	opOK         = "ok"
	opRemove     = "remove"
	opRename     = "rename"
)

type msg struct {
	Op   string `json:"op"`
	V    int    `json:"v,omitempty"`
	PSK  string `json:"psk,omitempty"`
	Msg  string `json:"msg,omitempty"`
	Path string `json:"path,omitempty"`
	// listResp
	Entries []wireEntry `json:"entries,omitempty"`
	// readHdr
	Size int64 `json:"size,omitempty"`
	// writeChk
	N int `json:"n,omitempty"`
	// rename
	NewPath string `json:"newPath,omitempty"`
}

type wireEntry struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModUnix int64  `json:"mod"`
	IsDir   bool   `json:"isDir"`
	Mode    uint32 `json:"mode"`
}

func readFrame(r io.Reader) ([]byte, error) {
	var nb [4]byte
	if _, err := io.ReadFull(r, nb[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(nb[:])
	if n == 0 || n > maxFrame {
		return nil, fmt.Errorf("invalid frame length %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func writeFrame(w io.Writer, payload []byte) error {
	if len(payload) == 0 || len(payload) > maxFrame {
		return fmt.Errorf("invalid payload length %d", len(payload))
	}
	var nb [4]byte
	binary.BigEndian.PutUint32(nb[:], uint32(len(payload)))
	if _, err := w.Write(nb[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func writeJSONFrame(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeFrame(w, b)
}

func readJSONFrame(r io.Reader, v any) error {
	b, err := readFrame(r)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
