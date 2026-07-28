package observability

func normalizeLabel(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
