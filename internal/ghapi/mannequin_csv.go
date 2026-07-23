package ghapi

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

// MannequinRecord is one row of the mannequin CSV: the source mannequin
// (login + node ID) and the target user login to reclaim it to. It is the
// single schema shared by the writer (`mannequin list`) and the reader
// (`mannequin claim --csv`), so the two can't drift.
type MannequinRecord struct {
	MannequinUser string
	MannequinID   string
	TargetUser    string
}

// mannequinCSVHeader is the CSV header row, in column order matching the fields
// of MannequinRecord.
var mannequinCSVHeader = []string{"mannequin-user", "mannequin-id", "target-user"}

// WriteMannequinCSV writes records as CSV (with the header row) to w using
// encoding/csv, so quoting and escaping are handled for us.
func WriteMannequinCSV(w io.Writer, records []MannequinRecord) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(mannequinCSVHeader); err != nil {
		return err
	}
	for _, r := range records {
		if err := cw.Write([]string{r.MannequinUser, r.MannequinID, r.TargetUser}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// ReadMannequinCSV parses a mannequin CSV from r using encoding/csv (so quoted
// fields, CRLF, and Excel exports round-trip), validates the header, and returns
// the data rows. It does not apply reclaim policy — that stays in ReclaimService.
func ReadMannequinCSV(r io.Reader) ([]MannequinRecord, error) {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true
	cr.FieldsPerRecord = len(mannequinCSVHeader)

	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parsing mannequin CSV: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if !headerMatches(rows[0]) {
		return nil, fmt.Errorf("invalid CSV header %q, expected %q",
			strings.Join(rows[0], ","), strings.Join(mannequinCSVHeader, ","))
	}

	records := make([]MannequinRecord, 0, len(rows)-1)
	for _, row := range rows[1:] {
		records = append(records, MannequinRecord{
			MannequinUser: strings.TrimSpace(row[0]),
			MannequinID:   strings.TrimSpace(row[1]),
			TargetUser:    strings.TrimSpace(row[2]),
		})
	}
	return records, nil
}

// ToMannequinRecords projects mannequins into CSV rows for `mannequin list`.
// When includeReclaimed is false, already-reclaimed mannequins are omitted.
func ToMannequinRecords(mannequins []Mannequin, includeReclaimed bool) []MannequinRecord {
	var records []MannequinRecord
	for _, m := range mannequins {
		if !includeReclaimed && m.MappedUser != nil {
			continue
		}
		target := ""
		if m.MappedUser != nil {
			target = m.MappedUser.Login
		}
		records = append(records, MannequinRecord{MannequinUser: m.Login, MannequinID: m.ID, TargetUser: target})
	}
	return records
}

func headerMatches(row []string) bool {
	if len(row) != len(mannequinCSVHeader) {
		return false
	}
	for i, h := range mannequinCSVHeader {
		if !strings.EqualFold(strings.TrimSpace(row[i]), h) {
			return false
		}
	}
	return true
}
