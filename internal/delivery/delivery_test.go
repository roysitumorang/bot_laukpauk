package delivery

import (
	"reflect"
	"testing"
	"time"
)

func labels(s []Slot) []string {
	out := make([]string, len(s))
	for i, x := range s {
		out[i] = x.Label
	}
	return out
}

func TestSlots(t *testing.T) {
	jkt, _ := time.LoadLocation("Asia/Jakarta")
	at := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 30, 0, 0, jkt) } // 2026-10-29 is a Thursday
	cases := []struct {
		now  time.Time
		want []string
	}{
		{at(29, 8), []string{"29-10-2026 – 12:00", "30-10-2026 – 08:00", "30-10-2026 – 10:00", "30-10-2026 – 12:00"}},
		{at(29, 15), []string{"30-10-2026 – 08:00", "30-10-2026 – 10:00", "30-10-2026 – 12:00"}},
		{at(29, 22), []string{"30-10-2026 – 12:00", "31-10-2026 – 08:00", "31-10-2026 – 10:00", "31-10-2026 – 12:00"}},
		{at(25, 23), []string{"26-10-2026 – 08:00", "26-10-2026 – 10:00", "26-10-2026 – 12:00"}}, // Sunday night
	}
	for _, c := range cases {
		if got := labels(Slots(c.now, nil)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Slots(%s) = %v, want %v", c.now, got, c.want)
		}
	}

	closed := func(d time.Time) bool { return d.Day() == 30 }
	if got := labels(Slots(at(29, 8), closed)); !reflect.DeepEqual(got, []string{"29-10-2026 – 12:00"}) {
		t.Errorf("closed days not skipped: %v", got)
	}
	s := Slots(at(29, 8), nil)[1]
	if s.Data != "delivery_30-10-2026_08:00" || LabelFromData(s.Data) != s.Label {
		t.Errorf("slot data %q / label %q", s.Data, LabelFromData(s.Data))
	}
	if len(s.Data) > 64 {
		t.Error("callback data too long")
	}
}
