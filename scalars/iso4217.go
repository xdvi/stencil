package scalars

import (
	"errors"
	"math"
	"strconv"
)

var (
	ErrInvalidCurrency  = errors.New("invalid ISO 4217 currency amount")
	ErrCurrencyOverflow = errors.New("currency amount overflows int64 cents")
)

const maxIntPartBeforeMul = (math.MaxInt64 - 99) / 100

// ParseCurrencyCents parses a formatted currency string to int64 cents with overflow protection.
func ParseCurrencyCents(b []byte) (int64, error) {
	i := 0
	n := len(b)
	for i < n && b[i] == ' ' {
		i++
	}
	for n > i && b[n-1] == ' ' {
		n--
	}
	if i >= n {
		return 0, ErrInvalidCurrency
	}

	negative := false
	if b[i] == '-' || b[i] == '+' {
		negative = b[i] == '-'
		i++
	}
	if i < n && b[i] == '$' {
		i++
	}
	if i >= n {
		return 0, ErrInvalidCurrency
	}

	var intPart int64
	var fracPart int64
	var intDigits, fracDigits int
	inFraction := false

	for ; i < n; i++ {
		c := b[i]
		switch {
		case c >= '0' && c <= '9':
			digit := int64(c - '0')
			if !inFraction {
				if intPart > (maxIntPartBeforeMul-digit)/10 {
					return 0, ErrCurrencyOverflow
				}
				intPart = intPart*10 + digit
				intDigits++
			} else {
				if fracDigits >= 2 {
					return 0, ErrInvalidCurrency
				}
				fracPart = fracPart*10 + digit
				fracDigits++
			}
		case c == ',':
			if inFraction || intDigits == 0 || i+1 >= n || b[i+1] < '0' || b[i+1] > '9' {
				return 0, ErrInvalidCurrency
			}
		case c == '.':
			if inFraction {
				return 0, ErrInvalidCurrency
			}
			inFraction = true
		default:
			return 0, ErrInvalidCurrency
		}
	}

	if intDigits == 0 && fracDigits == 0 {
		return 0, ErrInvalidCurrency
	}
	if inFraction && fracDigits == 0 {
		return 0, ErrInvalidCurrency
	}
	if fracDigits == 1 {
		fracPart *= 10
	}

	cents := intPart*100 + fracPart
	if negative {
		cents = -cents
	}
	return cents, nil
}

// AppendCurrencyCents formats cents to decimal notation in dst slice.
func AppendCurrencyCents(dst []byte, cents int64) []byte {
	if cents < 0 {
		dst = append(dst, '-')
		if cents == math.MinInt64 {
			return append(dst, "92233720368547758.08"...)
		}
		cents = -cents
	}
	whole := cents / 100
	rem := cents % 100
	dst = strconv.AppendInt(dst, whole, 10)
	dst = append(dst, '.')
	if rem < 10 {
		dst = append(dst, '0')
	}
	return strconv.AppendInt(dst, rem, 10)
}
