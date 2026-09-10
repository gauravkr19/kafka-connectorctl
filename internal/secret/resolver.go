package secret

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/example/connectorctl/internal/config"
)

type AppliedReference struct {
	ConfigKey string `json:"configKey" yaml:"configKey"`
	Reference string `json:"reference" yaml:"reference"`
}

var externalReferencePattern = regexp.MustCompile(`^\$\{(file|secret|env):[^{}\r\n]+\}$`)

type Resolver struct{ profiles config.SecretProfilesFile }

func NewResolver(profiles config.SecretProfilesFile) *Resolver { return &Resolver{profiles: profiles} }

func (r *Resolver) Resolve(connectorName string, cfg map[string]string, sensitiveKeys []string, mapping config.ConnectorMapping) (map[string]string, []AppliedReference, error) {
	out := clone(cfg)
	var applied []AppliedReference
	var unresolved []string
	for _, key := range sensitiveKeys {
		value := strings.TrimSpace(out[key])
		if IsExternalReference(value) {
			continue
		}
		field, ok := mapping.Fields[key]
		if !ok {
			unresolved = append(unresolved, key)
			continue
		}
		ref, err := r.referenceFor(field)
		if err != nil {
			return nil, nil, fmt.Errorf("connector %s secret mapping for %s: %w", connectorName, key, err)
		}
		out[key] = ref
		applied = append(applied, AppliedReference{ConfigKey: key, Reference: ref})
	}
	if len(unresolved) > 0 {
		sort.Strings(unresolved)
		return nil, nil, fmt.Errorf("connector %s has plaintext-sensitive fields without mappings: %s", connectorName, strings.Join(unresolved, ", "))
	}
	return out, applied, nil
}

func (r *Resolver) referenceFor(field config.SecretFieldRef) (string, error) {
	if field.LiteralReference != "" {
		if !IsExternalReference(field.LiteralReference) {
			return "", fmt.Errorf("literalReference is not an accepted external reference")
		}
		return field.LiteralReference, nil
	}
	profileName := field.Profile
	if profileName == "" {
		profileName = "mounted-file"
	}
	profile, ok := r.profiles.Profiles[profileName]
	if !ok {
		return "", fmt.Errorf("unknown secret profile %q", profileName)
	}
	if field.SecretKey == "" {
		return "", fmt.Errorf("secretKey is required")
	}
	mountPath := strings.TrimSpace(field.MountPath)
	if mountPath == "" {
		if field.SecretName == "" {
			return "", fmt.Errorf("secretName is required when mountPath is not supplied")
		}
		mountRoot := profile.MountRoot
		if mountRoot == "" {
			mountRoot = "/mnt/secrets"
		}
		fileName := field.FileName
		if fileName == "" {
			fileName = profile.DefaultFile
		}
		if fileName == "" {
			fileName = "custom.properties"
		}
		mountPath = path.Join(mountRoot, field.SecretName, fileName)
	}
	return fmt.Sprintf("${file:%s:%s}", mountPath, field.SecretKey), nil
}

func IsExternalReference(value string) bool {
	return externalReferencePattern.MatchString(strings.TrimSpace(value))
}

func clone(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
