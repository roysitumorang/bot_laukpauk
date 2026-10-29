// Package delivery computes the delivery slots offered at checkout.
package delivery

import (
	"strings"
	"time"
)

// CallbackPrefix prefixes the callback data of slot buttons.
const CallbackPrefix = "delivery_"

// Slot is one selectable delivery time.
type Slot struct {
	Label string // shown on the button, e.g. "30-10-2026 – 08:00"
	Data  string // callback data, e.g. "delivery_30-10-2026_08:00"
}

func slot(day time.Time, hour string) Slot {
	d := day.Format("02-01-2006")
	return Slot{Label: d + " – " + hour, Data: CallbackPrefix + d + "_" + hour}
}

// Slots returns the delivery options for an order placed at now (which must
// already be in the shop's time zone). Days for which closed(day) is true are
// skipped; closed may be nil.
//
// Rules (unchanged from the original bot):
//   - Sunday from 22:00: tomorrow 08:00, 10:00, 12:00
//   - before 10:00: today 12:00, tomorrow 08:00, 10:00, 12:00
//   - 10:00–21:59: tomorrow 08:00, 10:00, 12:00
//   - from 22:00: tomorrow 12:00, the day after 08:00, 10:00, 12:00
func Slots(now time.Time, closed func(time.Time) bool) []Slot {
	today := now
	besok := now.AddDate(0, 0, 1)
	lusa := now.AddDate(0, 0, 2)
	h := now.Hour()

	var out []Slot
	switch {
	case now.Weekday() == time.Sunday && h >= 22:
		out = []Slot{slot(besok, "08:00"), slot(besok, "10:00"), slot(besok, "12:00")}
	case h < 10:
		out = []Slot{slot(today, "12:00"), slot(besok, "08:00"), slot(besok, "10:00"), slot(besok, "12:00")}
	case h < 22:
		out = []Slot{slot(besok, "08:00"), slot(besok, "10:00"), slot(besok, "12:00")}
	default:
		out = []Slot{slot(besok, "12:00"), slot(lusa, "08:00"), slot(lusa, "10:00"), slot(lusa, "12:00")}
	}
	if closed == nil {
		return out
	}
	filtered := out[:0]
	for _, s := range out {
		day, err := time.ParseInLocation("02-01-2006", strings.SplitN(strings.TrimPrefix(s.Data, CallbackPrefix), "_", 2)[0], now.Location())
		if err == nil && closed(day) {
			continue
		}
		filtered = append(filtered, s)
	}
	return filtered
}

// LabelFromData turns callback data back into a readable label.
func LabelFromData(data string) string {
	v := strings.TrimPrefix(data, CallbackPrefix)
	return strings.Replace(v, "_", " – ", 1)
}
