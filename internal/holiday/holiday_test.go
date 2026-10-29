package holiday

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStore(t *testing.T) {
	jkt, _ := time.LoadLocation("Asia/Jakarta")
	path := filepath.Join(t.TempDir(), "holidays.json")
	s := New(path, jkt)
	if s.IsHoliday(time.Now()) || s.Describe() != "Tidak ada libur saat ini." {
		t.Fatal("empty store should have no holidays")
	}
	d := func(day int) time.Time { return time.Date(2026, 12, day, 0, 0, 0, 0, jkt) }
	if err := s.Add(d(24), d(26)); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(d(27), d(28)); err != nil { // adjacent: merged in Describe
		t.Fatal(err)
	}
	if err := s.Add(d(5), d(1)); err == nil {
		t.Error("reversed range should fail")
	}
	// End date is inclusive for the whole day.
	if !s.IsHoliday(time.Date(2026, 12, 26, 21, 0, 0, 0, jkt)) {
		t.Error("last day should be a holiday")
	}
	if s.IsHoliday(time.Date(2026, 12, 23, 23, 59, 0, 0, jkt)) {
		t.Error("day before should not be a holiday")
	}
	// UTC instant that is already the 24th in Jakarta.
	if !s.IsHoliday(time.Date(2026, 12, 23, 18, 0, 0, 0, time.UTC)) {
		t.Error("time zone not applied")
	}
	if got, want := s.Describe(), "24-12-2026 s/d 28-12-2026"; got != want {
		t.Errorf("Describe = %q, want %q", got, want)
	}
	if err := s.Clear(); err != nil || s.IsHoliday(d(25)) {
		t.Error("Clear failed")
	}
}

func TestLegacyFormat(t *testing.T) {
	jkt, _ := time.LoadLocation("Asia/Jakarta")
	path := filepath.Join(t.TempDir(), "holidays.json")
	// Written by the Python bot (both date formats occurred).
	os.WriteFile(path, []byte(`[{"start": "2026-01-01", "end": "2026-01-02"}, {"start": "10-01-2026", "end": "11-01-2026"}]`), 0o644)
	s := New(path, jkt)
	if !s.IsHoliday(time.Date(2026, 1, 2, 10, 0, 0, 0, jkt)) || !s.IsHoliday(time.Date(2026, 1, 10, 10, 0, 0, 0, jkt)) {
		t.Error("legacy file not understood")
	}
}
