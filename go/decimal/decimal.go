// Package decimal centralizes exact decimal-string arithmetic for money paths.
//
// The Go→Haskell IR contract requires canonical plain decimal strings
// (optional '-', digits, optional fraction — never scientific notation).
// Sources do not honor that on their own: Robinhood CSVs carry values like
// "1e-05" and CoinGecko returns JSON floats. Everything that lands in the IR
// must pass through Canon, which parses exactly and re-renders canonically.
package decimal

import (
	"fmt"
	"math/big"
	"strings"
)

// Parse parses a decimal string (plain or scientific notation) into an exact
// rational. big.Rat.SetString alone is too permissive (hex/binary literals,
// underscores, fractions), so the shape is validated first.
func Parse(s string) (*big.Rat, error) {
	trimmed := strings.TrimSpace(s)
	if !isDecimalShaped(trimmed) {
		return nil, fmt.Errorf("invalid decimal %q", s)
	}
	r, ok := new(big.Rat).SetString(trimmed)
	if !ok {
		return nil, fmt.Errorf("invalid decimal %q", s)
	}
	return r, nil
}

// isDecimalShaped accepts [sign] digits [. digits] [e/E [sign] digits],
// requiring at least one mantissa digit.
func isDecimalShaped(s string) bool {
	i, n := 0, len(s)
	if i < n && (s[i] == '+' || s[i] == '-') {
		i++
	}
	digits := 0
	for i < n && s[i] >= '0' && s[i] <= '9' {
		i, digits = i+1, digits+1
	}
	if i < n && s[i] == '.' {
		i++
		for i < n && s[i] >= '0' && s[i] <= '9' {
			i, digits = i+1, digits+1
		}
	}
	if digits == 0 {
		return false
	}
	if i < n && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < n && (s[i] == '+' || s[i] == '-') {
			i++
		}
		expDigits := 0
		for i < n && s[i] >= '0' && s[i] <= '9' {
			i, expDigits = i+1, expDigits+1
		}
		if expDigits == 0 {
			return false
		}
	}
	return i == n
}

// String renders a rational as a canonical plain decimal string with no
// precision loss: no exponent, no trailing fractional zeros, "0" for zero.
// The denominator must contain only factors of 2 and 5 (always true for
// values parsed from decimal strings); anything else panics, because
// rendering it would silently lose precision.
func String(r *big.Rat) string {
	scale := terminatingScale(r)
	out := r.FloatString(scale)
	if strings.Contains(out, ".") {
		out = strings.TrimRight(out, "0")
		out = strings.TrimSuffix(out, ".")
	}
	if out == "-0" || out == "" {
		return "0"
	}
	return out
}

// Canon parses a decimal string and re-renders it canonically.
func Canon(s string) (string, error) {
	r, err := Parse(s)
	if err != nil {
		return "", err
	}
	return String(r), nil
}

// Mul multiplies two decimal strings exactly.
func Mul(a, b string) (string, error) {
	ra, err := Parse(a)
	if err != nil {
		return "", err
	}
	rb, err := Parse(b)
	if err != nil {
		return "", err
	}
	return String(new(big.Rat).Mul(ra, rb)), nil
}

// Sign reports the sign of a decimal string (-1, 0, +1).
func Sign(s string) (int, error) {
	r, err := Parse(s)
	if err != nil {
		return 0, err
	}
	return r.Sign(), nil
}

// Abs returns the absolute value of a decimal string, canonicalized.
func Abs(s string) (string, error) {
	r, err := Parse(s)
	if err != nil {
		return "", err
	}
	return String(new(big.Rat).Abs(r)), nil
}

// terminatingScale returns the number of fractional digits needed to render
// r exactly. Panics if the reduced denominator has prime factors other than
// 2 and 5 (a non-terminating decimal).
func terminatingScale(r *big.Rat) int {
	d := new(big.Int).Set(r.Denom())
	two := big.NewInt(2)
	five := big.NewInt(5)
	rem := new(big.Int)

	twos := 0
	for {
		q, m := new(big.Int).QuoRem(d, two, rem)
		if m.Sign() != 0 {
			break
		}
		d, twos = q, twos+1
	}
	fives := 0
	for {
		q, m := new(big.Int).QuoRem(d, five, rem)
		if m.Sign() != 0 {
			break
		}
		d, fives = q, fives+1
	}
	if d.Cmp(big.NewInt(1)) != 0 {
		panic(fmt.Sprintf("decimal.String: non-terminating decimal with denominator %s", r.Denom()))
	}
	if twos > fives {
		return twos
	}
	return fives
}
