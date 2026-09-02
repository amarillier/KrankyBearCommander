package fsops

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// hashFileForVerify is HashFile by default — verifyDestHash calls it
// indirectly through this variable so tests can substitute a fake that
// simulates a verification mismatch without needing real disk corruption.
var hashFileForVerify = HashFile

// HashFile returns path's sha256 hex digest, streamed in 256KB chunks (same
// buffer size copyFile uses) so a large file never needs to fit in memory at
// once. progress, if non-nil, is called with the cumulative bytes read after
// each chunk; a false return cancels early with ErrCancelled. Shared by
// CopyVerify/MoveVerify (re-hashing a just-written destination file) and
// FindDuplicates (hashing every candidate file).
func HashFile(path string, progress func(read int64) bool) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	buf := make([]byte, 256*1024)
	var readTotal int64
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
			readTotal += int64(n)
			if progress != nil && !progress(readTotal) {
				return "", ErrCancelled
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", rerr
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
