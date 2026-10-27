//go:build !darwin

package sessions

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotCopyBudget(t *testing.T) {
	old := cursorCopyLimit
	t.Cleanup(func() { cursorCopyLimit = old })
	for _, tc := range []struct {
		name             string
		main, wal, limit int
		reject           bool
	}{
		{"large-main", 10, 1, 9, true}, {"combined-budget", 5, 5, 9, true},
		{"exact-budget", 5, 4, 9, false}, {"below-budget", 3, 2, 9, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			temp := filepath.Join(root, "temp")
			if err := os.Mkdir(temp, 0700); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"TMP", "TEMP", "TMPDIR"} {
				t.Setenv(key, temp)
			}
			p := filepath.Join(root, "source.db")
			main := bytes.Repeat([]byte("a"), tc.main)
			wal := bytes.Repeat([]byte("b"), tc.wal)
			if err := os.WriteFile(p, main, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p+"-wal", wal, 0600); err != nil {
				t.Fatal(err)
			}
			cursorCopyLimit = int64(tc.limit)
			opens := 0
			_, dir, err := snapshotCursorStore(p, func(string, string) (*sql.DB, error) { opens++; return nil, nil })
			if dir != "" {
				t.Cleanup(func() { os.RemoveAll(dir) })
			}
			if tc.reject {
				if !errors.Is(err, errCursorCopyLimit) || opens != 0 {
					t.Fatalf("oversized snapshot reached open: error=%v opens=%d", err, opens)
				}
				entries, e := os.ReadDir(temp)
				if e != nil || len(entries) != 0 {
					t.Fatalf("partial snapshot left behind: %v %v", entries, e)
				}
			} else {
				if err != nil || opens != 1 {
					t.Fatalf("bounded snapshot failed: %v opens=%d", err, opens)
				}
				copied, e := os.ReadFile(filepath.Join(dir, "store.db"))
				if e != nil || !bytes.Equal(copied, main) {
					t.Fatal("database copy differs")
				}
			}
			got, e := os.ReadFile(p)
			if e != nil || !bytes.Equal(got, main) {
				t.Fatal("source DB changed")
			}
			got, e = os.ReadFile(p + "-wal")
			if e != nil || !bytes.Equal(got, wal) {
				t.Fatal("source WAL changed")
			}
		})
	}
}

func TestCopyBudgetStopsGrowingInput(t *testing.T) {
	var out bytes.Buffer
	n, err := copyCursorBytes(&out, bytes.NewBufferString("abcdef"), 3)
	if n != 3 || out.String() != "abc" || !errors.Is(err, errCursorCopyLimit) {
		t.Fatalf("limit not enforced: n=%d data=%q err=%v", n, out.String(), err)
	}
}
