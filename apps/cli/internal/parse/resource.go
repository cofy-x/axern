package parse

import (
	"fmt"
	"math/big"
	"strings"
)

// CPU parses a user-facing CPU quantity into milli CPU units. Bare values are
// CPU cores, and values with an m suffix are milli CPU.
func CPU(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	if strings.HasPrefix(value, "-") {
		return 0, fmt.Errorf("cpu quantity must be >= 0")
	}
	if strings.HasSuffix(value, "m") {
		return parseScaledDecimal(strings.TrimSuffix(value, "m"), 1, "cpu quantity")
	}
	return parseScaledDecimal(value, 1000, "cpu quantity")
}

// Memory parses a user-facing memory quantity into bytes. Bare values are
// bytes. Binary and decimal suffixes are supported, for example 512Mi, 512MiB,
// or 1GB.
func Memory(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	if strings.HasPrefix(value, "-") {
		return 0, fmt.Errorf("memory quantity must be >= 0")
	}
	number, factor, ok := splitMemoryQuantity(value)
	if !ok {
		return 0, fmt.Errorf("memory quantity has unsupported unit")
	}
	return parseScaledDecimal(number, factor, "memory quantity")
}

func splitMemoryQuantity(value string) (string, int64, bool) {
	units := []struct {
		suffix string
		factor int64
	}{
		{suffix: "tib", factor: 1024 * 1024 * 1024 * 1024},
		{suffix: "gib", factor: 1024 * 1024 * 1024},
		{suffix: "mib", factor: 1024 * 1024},
		{suffix: "kib", factor: 1024},
		{suffix: "ti", factor: 1024 * 1024 * 1024 * 1024},
		{suffix: "gi", factor: 1024 * 1024 * 1024},
		{suffix: "mi", factor: 1024 * 1024},
		{suffix: "ki", factor: 1024},
		{suffix: "tb", factor: 1000 * 1000 * 1000 * 1000},
		{suffix: "gb", factor: 1000 * 1000 * 1000},
		{suffix: "mb", factor: 1000 * 1000},
		{suffix: "kb", factor: 1000},
		{suffix: "b", factor: 1},
	}
	lower := strings.ToLower(value)
	for _, unit := range units {
		if strings.HasSuffix(lower, unit.suffix) {
			return strings.TrimSpace(value[:len(value)-len(unit.suffix)]), unit.factor, true
		}
	}
	return value, 1, true
}

func parseScaledDecimal(value string, scale int64, label string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("%s is required", label)
	}
	if strings.HasPrefix(value, "+") {
		value = value[1:]
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" && len(parts) == 1 {
		return 0, fmt.Errorf("%s must be a decimal number", label)
	}
	whole := parts[0]
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if whole == "" {
		whole = "0"
	}
	if fraction == "" && strings.HasSuffix(value, ".") {
		return 0, fmt.Errorf("%s must be a decimal number", label)
	}
	digits := whole + fraction
	for _, r := range digits {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("%s must be a decimal number", label)
		}
	}
	numerator, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return 0, fmt.Errorf("%s must be a decimal number", label)
	}
	numerator.Mul(numerator, big.NewInt(scale))
	denominator := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(len(fraction))), nil)
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	if remainder.Sign() != 0 {
		return 0, fmt.Errorf("%s must resolve to a whole unit", label)
	}
	if !quotient.IsInt64() {
		return 0, fmt.Errorf("%s is too large", label)
	}
	return quotient.Int64(), nil
}
