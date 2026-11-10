package ledger

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestExpensePairUpgradeDigestPreservesInvalidUTF8TextBytes(t *testing.T) {
	digests := [][32]byte{}
	for _, hex := range []string{"ff", "fe"} {
		db, e := sql.Open("sqlite", filepath.Join(t.TempDir(), "synthetic.db"))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec("CREATE TABLE items(value TEXT); INSERT INTO items VALUES(CAST(x'" + hex + "' AS TEXT))"); e != nil {
			db.Close()
			t.Fatal(e)
		}
		d, e := expensePairUpgradeDigest(db)
		db.Close()
		if e != nil {
			t.Fatal(e)
		}
		digests = append(digests, d)
	}
	if digests[0] == digests[1] {
		t.Fatal("distinct invalid UTF8 text bytes collapsed")
	}
}
