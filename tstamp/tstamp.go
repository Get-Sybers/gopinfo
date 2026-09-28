// Package tstamp normalises artefact timestamps to the envelope's ISO 8601
// form (docs/linux §3.3): UTC, fixed microsecond precision —
// 2006-01-02T15:04:05.000000Z — so every timestamp in every record is
// uniform and lexically sortable. Parsers hand byakugan the native truth;
// what this package fixes is only the *rendering*: one time format across
// every tool.
//
// Naive timestamps (a syslog line has no zone) are recorded as UTC verbatim —
// the imaged host's zone is context byakugan applies (gohost extracts it),
// never guessed here.
package tstamp

import (
	"strings"
	"time"
)

// ISO8601Layout is the one timestamp rendering of the Linux matrix: ISO
// 8601, UTC, fixed microseconds.
const ISO8601Layout = "2006-01-02T15:04:05.000000Z"

// ISO8601 renders t in the fixed layout; a zero time renders as "".
func ISO8601(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(ISO8601Layout)
}

// Unix renders an epoch seconds + nanoseconds pair; zero renders as "".
func Unix(sec, nsec int64) string {
	if sec == 0 && nsec == 0 {
		return ""
	}
	return ISO8601(time.Unix(sec, nsec))
}

// UnixMicros renders an epoch in microseconds (journal __REALTIME).
func UnixMicros(us int64) string {
	if us == 0 {
		return ""
	}
	return ISO8601(time.Unix(us/1e6, (us%1e6)*1e3))
}

// Days renders a days-since-epoch count (shadow(5) fields); 0 and negative
// render as "".
func Days(days int64) string {
	if days <= 0 {
		return ""
	}
	return ISO8601(time.Unix(days*86400, 0))
}

// Syslog3164 parses the classic yearless syslog prefix ("Jan  2 15:04:05")
// against a reference time — the log file's mtime. The year is the reference
// year unless that puts the event more than 48 hours after the reference, in
// which case it is the year before (the December-log-read-in-January case).
// The result is naive-as-UTC per the package rule.
func Syslog3164(s string, ref time.Time) (time.Time, bool) {
	return Syslog3164In(s, ref, nil)
}

// Syslog3164In is Syslog3164 with the host's zone from the Layer-1
// knowledge store (decision 14): a non-nil loc interprets the naive
// prefix as host-local time — the rendered ISO 8601 UTC is then the real
// instant. nil keeps the naive-as-UTC rule.
func Syslog3164In(s string, ref time.Time, loc *time.Location) (time.Time, bool) {
	if loc == nil {
		loc = time.UTC
	}
	t, err := time.Parse("Jan _2 15:04:05", strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, false
	}
	if ref.IsZero() {
		ref = time.Now()
	}
	// The wall clock in s belongs to the host's calendar, so the inferred
	// year is ref's year IN loc — ref.UTC().Year() would be a year off
	// around New Year for large offsets (a +14:00 host is already in the
	// new year while UTC is not).
	ref = ref.In(loc)
	t = time.Date(ref.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, loc)
	if t.After(ref.Add(48 * time.Hour)) {
		t = t.AddDate(-1, 0, 0)
	}
	return t, true
}

// flexLayouts are the self-describing timestamp dialects Flexible accepts, in
// trial order: RFC3339/5424 (with or without sub-seconds and zone), the
// space-separated ISO variants rsyslog and application logs write, and the
// zone-less forms recorded naive-as-UTC.
var flexLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999-0700",
	"2006-01-02 15:04:05.999999999-07",
	"2006-01-02T15:04:05.999999999-07",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999-0700",
}

// Flexible parses a self-describing timestamp string in the common log
// dialects; ok is false when none match.
func Flexible(s string) (time.Time, bool) {
	return FlexibleIn(s, nil)
}

// FlexibleIn is Flexible with the host's zone for the zone-LESS layouts
// (decision 14): a form carrying its own offset is unaffected; a naive
// form is interpreted in loc when non-nil, else as UTC.
func FlexibleIn(s string, loc *time.Location) (time.Time, bool) {
	if loc == nil {
		loc = time.UTC
	}
	s = strings.TrimSpace(s)
	for _, l := range flexLayouts {
		if t, err := time.ParseInLocation(l, s, loc); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// RFC3339Nano renders t as UTC RFC3339 with nanosecond precision, or "" for the
// zero time — the rendering the Windows artefact records use.
func RFC3339Nano(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
