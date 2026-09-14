// database-backup creates verified snapshots or restores into a new database file.
package main

import (
	"flag"
	"fmt"
	"github.com/letrahoo/monee/server/internal/storage"
	"os"
	"path/filepath"
)

func main() {
	source := flag.String("source", "", "SQLite database to snapshot")
	backup := flag.String("restore", "", "backup directory to restore")
	out := flag.String("out", "", "new output directory (snapshot) or file (restore)")
	flag.Parse()
	if (*source == "") == (*backup == "") || *out == "" {
		fmt.Fprintln(os.Stderr, "use -source DB -out NEW_DIRECTORY or -restore BACKUP -out NEW_DB")
		os.Exit(2)
	}
	target, e := filepath.Abs(*out)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	if *source != "" {
		var src string
		src, e = filepath.Abs(*source)
		if e == nil {
			e = storage.Snapshot(src, target)
		}
	} else {
		e = storage.Restore(*backup, target)
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	fmt.Println("Verified database operation completed; existing files were not overwritten.")
}
