// Package holiday stores the shop's closing periods ("toko libur").
//
// The data lives in a small JSON file (same format as the original
// holidays.json: [{"start": "2025-01-01", "end": "2025-01-03"}]).
package holiday

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Range is one closing period; both ends are inclusive.
type Range struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// Store reads and writes the holiday file.
type Store struct {
	path string
	loc  *time.Location
	mu   sync.Mutex
}

// New returns a Store backed by path. Dates are interpreted in loc.
func New(path string, loc *time.Location) *Store {
	return &Store{path: path, loc: loc}
}

func (s *Store) load() []Range {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return nil
	}
	var rs []Range
	if err := json.Unmarshal(b, &rs); err != nil {
		return nil
	}
	return rs
}

func (s *Store) save(rs []Range) error {
	if rs == nil {
		rs = []Range{}
	}
	b, err := json.MarshalIndent(rs, "", "    ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(s.path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// ParseDate accepts YYYY-MM-DD or DD-MM-YYYY.
func ParseDate(v string, loc *time.Location) (time.Time, error) {
	v = strings.TrimSpace(v)
	for _, layout := range []string{"2006-01-02", "02-01-2006"} {
		if t, err := time.ParseInLocation(layout, v, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("format tanggal tidak dikenali: %q", v)
}

type span struct{ start, end time.Time }

func (s *Store) spans() []span {
	var out []span
	for _, r := range s.load() {
		st, err1 := ParseDate(r.Start, s.loc)
		en, err2 := ParseDate(r.End, s.loc)
		if err1 != nil || err2 != nil {
			continue
		}
		if en.Before(st) {
			st, en = en, st
		}
		out = append(out, span{st, en})
	}
	return out
}

func dateOnly(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// IsHoliday reports whether the calendar day of t falls in any closing period.
func (s *Store) IsHoliday(t time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := dateOnly(t, s.loc)
	for _, sp := range s.spans() {
		if !d.Before(sp.start) && !d.After(sp.end) {
			return true
		}
	}
	return false
}

// Add appends a closing period.
func (s *Store) Add(start, end time.Time) error {
	if end.Before(start) {
		return errors.New("tanggal selesai lebih awal dari tanggal mulai")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rs := append(s.load(), Range{
		Start: start.In(s.loc).Format("2006-01-02"),
		End:   end.In(s.loc).Format("2006-01-02"),
	})
	return s.save(rs)
}

// Clear removes all closing periods (toko buka kembali).
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(nil)
}

// Describe returns the merged periods as "DD-MM-YYYY s/d DD-MM-YYYY | ...".
func (s *Store) Describe() string {
	s.mu.Lock()
	sp := s.spans()
	s.mu.Unlock()
	if len(sp) == 0 {
		return "Tidak ada libur saat ini."
	}
	sort.Slice(sp, func(i, j int) bool { return sp[i].start.Before(sp[j].start) })
	merged := []span{sp[0]}
	for _, x := range sp[1:] {
		last := &merged[len(merged)-1]
		if !x.start.After(last.end.AddDate(0, 0, 1)) {
			if x.end.After(last.end) {
				last.end = x.end
			}
			continue
		}
		merged = append(merged, x)
	}
	parts := make([]string, len(merged))
	for i, m := range merged {
		parts[i] = m.start.Format("02-01-2006") + " s/d " + m.end.Format("02-01-2006")
	}
	return strings.Join(parts, " | ")
}
