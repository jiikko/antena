package feed

import (
	"regexp"
	"strings"
	"time"
)

// ParseDate reads feed dates. Feedjira uses Ruby's DateTime.parse, which is
// lenient; this accepts the RFC 822 / RFC 1123 and ISO 8601 / W3CDTF forms
// feeds use, with the zone names DateTime.parse knows for them. The result is
// UTC with microseconds, like Feedjira's feed_utils_to_gm_time.
func ParseDate(s string) (time.Time, bool) {
	v := strings.TrimSpace(s)
	v = zoneName.ReplaceAllStringFunc(v, func(z string) string { return zones[strings.ToUpper(z)] })
	for _, layout := range layouts {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC().Truncate(time.Microsecond), true
		}
	}
	return time.Time{}, false
}

var layouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05Z0700",
	"2006-01-02T15:04:05.999999999Z0700",
	"2006-01-02T15:04Z07:00",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05Z07:00",
	"2006-01-02 15:04:05 -0700",
	"2006-01-02 15:04:05",
	"2006-01-02",
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04 -0700",
	"Mon,2 Jan 2006 15:04:05 -0700",
	"2 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04:05",
	"Mon, 2 January 2006 15:04:05 -0700",
}

// Zone names at the end of RFC 822 dates, rewritten to numeric offsets
// (Go's time.Parse keeps an unknown abbreviation at offset 0).
var zoneName = regexp.MustCompile(`(?i)\b(UT|UTC|GMT|Z|JST|EST|EDT|CST|CDT|MST|MDT|PST|PDT)$`)

var zones = map[string]string{
	"UT": "+0000", "UTC": "+0000", "GMT": "+0000", "Z": "+0000", "JST": "+0900",
	"EST": "-0500", "EDT": "-0400", "CST": "-0600", "CDT": "-0500",
	"MST": "-0700", "MDT": "-0600", "PST": "-0800", "PDT": "-0700",
}
