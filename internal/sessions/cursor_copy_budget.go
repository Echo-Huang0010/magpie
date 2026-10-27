package sessions

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// Clones share storage, but a failed clone must not turn a live multi-GB
// database into an unbounded full copy on every refresh.
var cursorCopyLimit int64 = 64 << 20

var errCursorCopyLimit = errors.New("database snapshot exceeds the full-copy byte limit")

func copyCursorFile(from, to string, limit int64) (int64, error) {
	in, err := os.Open(from)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return 0, err
	}
	if limit < 0 || fi.Size() > limit {
		return 0, fmt.Errorf("%w: file is %d bytes, %d remain", errCursorCopyLimit, fi.Size(), limit)
	}
	out, err := os.Create(to)
	if err != nil {
		return 0, err
	}
	n, err := copyCursorBytes(out, in, limit)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	return n, err
}

// Check the stream as well as Stat: Cursor can grow the file while it is
// being copied. Read one extra byte to detect that, but never write it.
func copyCursorBytes(out io.Writer, in io.Reader, limit int64) (int64, error) {
	if limit < 0 {
		return 0, errCursorCopyLimit
	}
	n, err := io.CopyN(out, in, limit)
	if err == io.EOF {
		return n, nil
	}
	if err != nil {
		return n, err
	}
	var extra [1]byte
	read, err := io.ReadFull(in, extra[:])
	if read != 0 {
		return n, errCursorCopyLimit
	}
	if err == io.EOF {
		return n, nil
	}
	return n, err
}
