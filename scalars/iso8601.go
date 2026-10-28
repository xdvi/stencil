package scalars

import (
	"errors"
	"time"
)

var ErrInvalidISO8601 = errors.New("invalid ISO 8601 UTC timestamp (expected YYYY-MM-DDTHH:MM:SSZ)")

// ParseISO8601 parses YYYY-MM-DDTHH:MM:SSZ with zero heap allocations and strict boundary validation.
func ParseISO8601(b []byte) (time.Time, error) {
	if len(b) != 20 || b[4] != '-' || b[7] != '-' || b[10] != 'T' || b[13] != ':' || b[16] != ':' || b[19] != 'Z' {
		return time.Time{}, ErrInvalidISO8601
	}

	year, okYear := parseDigits(b[0:4])
	month, okMonth := parseDigits(b[5:7])
	day, okDay := parseDigits(b[8:10])
	hour, okHour := parseDigits(b[11:13])
	min, okMin := parseDigits(b[14:16])
	sec, okSec := parseDigits(b[17:19])

	if !okYear || !okMonth || !okDay || !okHour || !okMin || !okSec {
		return time.Time{}, ErrInvalidISO8601
	}

	if year < 1 || month < 1 || month > 12 || day < 1 || day > maxDaysInMonth(year, month) ||
		hour > 23 || min > 59 || sec > 59 {
		return time.Time{}, ErrInvalidISO8601
	}

	return time.Date(year, time.Month(month), day, hour, min, sec, 0, time.UTC), nil
}

// AppendISO8601 appends RFC 3339 / ISO 8601 UTC timestamp to dst buffer.
func AppendISO8601(dst []byte, t time.Time) []byte {
	return t.UTC().AppendFormat(dst, "2006-01-02T15:04:05Z")
}

func maxDaysInMonth(year, month int) int {
	switch month {
	case 1, 3, 5, 7, 8, 10, 12:
		return 31
	case 4, 6, 9, 11:
		return 30
	case 2:
		if (year%4 == 0 && year%100 != 0) || (year%400 == 0) {
			return 29
		}
		return 28
	default:
		return 0
	}
}

// parseDigits converts a fixed-width ASCII digit sequence to an int.
// The second result reports whether every byte is a decimal digit.
func parseDigits(b []byte) (int, bool) {
	n := 0
	for _, c := range b {
		d := int(c) - '0'
		if d < 0 || d > 9 {
			return 0, false
		}
		n = n*10 + d
	}
	return n, true
}
