package transform

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var invalidDNSLabel = regexp.MustCompile(`[^a-z0-9-]+`)
var repeatedDash = regexp.MustCompile(`-+`)

// KubernetesName returns a stable DNS-1123 label. The exact Kafka connector
// name remains in spec.name, so shortening or normalizing metadata.name is safe.
func KubernetesName(connectorName string) string {
	original := strings.TrimSpace(connectorName)
	name := strings.ToLower(original)
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, ".", "-")
	name = invalidDNSLabel.ReplaceAllString(name, "-")
	name = repeatedDash.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	changed := name != original
	if name == "" {
		name = "connector"
		changed = true
	}
	const maxNameLength = 63
	if len(name) > maxNameLength {
		changed = true
	}
	if changed {
		sum := sha256.Sum256([]byte(original))
		suffix := hex.EncodeToString(sum[:])[:10]
		maxBase := maxNameLength - 1 - len(suffix)
		if len(name) > maxBase {
			name = name[:maxBase]
		}
		name = strings.Trim(name, "-")
		if name == "" {
			name = "connector"
		}
		name += "-" + suffix
	}
	return name
}
