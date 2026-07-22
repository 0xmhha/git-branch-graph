// Package csvw writes RFC4180 CSV files (comma-delimited, quoted as needed).
//
// git output is parsed using 0x1F field separators to avoid collisions, but the
// persisted raw/*.csv files use standard CSV quoting so any tool (pandas, sqlite
// .import, Excel) can load them.
package csvw

import (
	"encoding/csv"
	"os"
)

// Write creates path and writes header + rows as RFC4180 CSV.
func Write(path string, header []string, rows [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if err := w.Write(header); err != nil {
		return err
	}
	if err := w.WriteAll(rows); err != nil { // WriteAll flushes
		return err
	}
	return w.Error()
}
