package oci

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// ConfigurationDigest identifies the immutable policy used by this loader, not
// a file that could have changed since the handler was constructed.
func (r *BundleLoader) ConfigurationDigest() (string, error) {
	payload, err := json.Marshal([]any{r.baseSpec, r.specBuilder.profile, r.runtimeFiles})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
