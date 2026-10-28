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

	year := parse4(b[0:4])
	month := parse2(b[5:7])
	day := parse2(b[8:10])
	hour := parse2(b[11:13])
	min := parse2(b[14:16])
	sec := parse2(b[17:19])

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

func parse4(b []byte) int {
	d0, d1, d2, d3 := int(b[0]-'0'), int(b[1]-'0'), int(b[2]-'0'), int(b[3]-'0')
	if d0 < 0 || d0 > 9 || d1 < 0 || d1 > 9 || d2 < 0 || d2 > 9 || d3 < 0 || d3 > 9 {
		return -1
	}
	return d0*1000 + d1*100 + d2*10 + d3
}

func parse2(b []byte) int {
	d0, d1 := int(b[0]-'0'), int(b[1]-'0')
	if d0 < 0 || d0 > 9 || d1 < 0 || d1 > 9 {
		return -1
	}
	return d0*10 + d1
}
