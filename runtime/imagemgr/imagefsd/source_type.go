package imagefsd

const (
	SourceTypeOSS   = "oss"
	SourceTypeNydus = "nydus"
)

func normalizeSourceType(sourceType string) string {
	if sourceType == "" {
		return SourceTypeOSS
	}
	return sourceType
}
