package axernsdk

import (
	"math/big"
	"strconv"
	"strings"
)

// ResourceQuantity is a user-facing resource quantity. String literals such as
// "500m", "512Mi", and "512MiB" can be assigned directly; constructors below
// make numeric quantities explicit without weakening the public API to any.
type ResourceQuantity string

// CPUCores formats CPU cores as a resource quantity.
func CPUCores(cores int64) ResourceQuantity {
	return ResourceQuantity(strconv.FormatInt(cores, 10))
}

// CPUMilli formats milli CPU as a resource quantity.
func CPUMilli(milli int64) ResourceQuantity {
	return ResourceQuantity(strconv.FormatInt(milli, 10) + "m")
}

// MemoryBytes formats memory bytes as a resource quantity.
func MemoryBytes(bytes int64) ResourceQuantity {
	return ResourceQuantity(strconv.FormatInt(bytes, 10))
}

// EphemeralStorageBytes formats node-local ephemeral storage bytes as a resource quantity.
func EphemeralStorageBytes(bytes int64) ResourceQuantity {
	return ResourceQuantity(strconv.FormatInt(bytes, 10))
}

func parseCPUQuantity(field string, quantity ResourceQuantity) (int64, error) {
	value := strings.TrimSpace(string(quantity))
	if value == "" {
		return 0, nil
	}
	if strings.HasPrefix(value, "-") {
		return 0, positiveQuantityError(field)
	}
	if strings.HasSuffix(value, "m") {
		return parseScaledResourceDecimal(field, strings.TrimSuffix(value, "m"), 1)
	}
	return parseScaledResourceDecimal(field, value, 1000)
}

func parseMemoryQuantity(field string, quantity ResourceQuantity) (int64, error) {
	value := strings.TrimSpace(string(quantity))
	if value == "" {
		return 0, nil
	}
	if strings.HasPrefix(value, "-") {
		return 0, positiveQuantityError(field)
	}
	number, factor, ok := splitMemoryQuantity(value)
	if !ok {
		return 0, &ValidationError{Field: field, Message: "unsupported unit"}
	}
	return parseScaledResourceDecimal(field, number, factor)
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

func parseScaledResourceDecimal(field, value string, scale int64) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, &ValidationError{Field: field, Message: "is required"}
	}
	value = strings.TrimPrefix(value, "+")
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" && len(parts) == 1 {
		return 0, &ValidationError{Field: field, Message: "must be a decimal number"}
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
		return 0, &ValidationError{Field: field, Message: "must be a decimal number"}
	}
	digits := whole + fraction
	for _, r := range digits {
		if r < '0' || r > '9' {
			return 0, &ValidationError{Field: field, Message: "must be a decimal number"}
		}
	}
	numerator, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return 0, &ValidationError{Field: field, Message: "must be a decimal number"}
	}
	numerator.Mul(numerator, big.NewInt(scale))
	denominator := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(len(fraction))), nil)
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	if remainder.Sign() != 0 {
		return 0, &ValidationError{Field: field, Message: "must resolve to a whole unit"}
	}
	if !quotient.IsInt64() {
		return 0, &ValidationError{Field: field, Message: "is too large"}
	}
	return quotient.Int64(), nil
}

func positiveQuantityError(field string) error {
	return &ValidationError{Field: field, Message: "must be non-negative"}
}
