package dashboard

const (
	DefaultEventLimit = 50
	MaxEventLimit     = 500
)

func NormalizeLimit(limit int32) int32 {
	if limit <= 0 {
		return DefaultEventLimit
	}
	if limit > MaxEventLimit {
		return MaxEventLimit
	}
	return limit
}
