package config

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

var (
	kubernetesSecretName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)
	kubernetesSecretKey  = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	externalReference    = regexp.MustCompile(`^\$\{(file|secret|env):[^{}\r\n]+\}$`)
)

func ValidateSecretMappings(mappings SecretMappingsFile, profiles SecretProfilesFile) error {
	var problems []string
	if mappings.APIVersion != RegistryAPIVersion {
		problems = append(problems, fmt.Sprintf("apiVersion must be %q", RegistryAPIVersion))
	}
	for connectorName, mapping := range mappings.Connectors {
		prefix := fmt.Sprintf("connector %q", connectorName)
		if strings.TrimSpace(connectorName) == "" || strings.ContainsAny(connectorName, "\r\n") {
			problems = append(problems, prefix+": name is empty or contains a newline")
		}
		if mapping.ConnectProfile != "" && !safeKey.MatchString(mapping.ConnectProfile) {
			problems = append(problems, prefix+": connectProfile must be a lowercase profile key")
		}
		for configKey, field := range mapping.Fields {
			fieldPrefix := fmt.Sprintf("%s field %q", prefix, configKey)
			if strings.TrimSpace(configKey) == "" || strings.ContainsAny(configKey, "\r\n") {
				problems = append(problems, fieldPrefix+": configuration key is invalid")
			}
			if field.LiteralReference != "" {
				if !externalReference.MatchString(strings.TrimSpace(field.LiteralReference)) {
					problems = append(problems, fieldPrefix+": literalReference is not a supported ${file:...}, ${secret:...}, or ${env:...} reference")
				}
				if field.SecretName != "" || field.SecretKey != "" || field.FileName != "" || field.MountPath != "" {
					problems = append(problems, fieldPrefix+": literalReference cannot be combined with secretName, secretKey, fileName, or mountPath")
				}
				continue
			}
			profileName := field.Profile
			if profileName == "" {
				profileName = "mounted-file"
			}
			if _, ok := profiles.Profiles[profileName]; !ok {
				problems = append(problems, fmt.Sprintf("%s: unknown secret profile %q", fieldPrefix, profileName))
			}
			if !kubernetesSecretKey.MatchString(field.SecretKey) {
				problems = append(problems, fieldPrefix+": secretKey must contain only letters, digits, '.', '_' or '-'")
			}
			if field.MountPath == "" {
				if len(field.SecretName) > 253 || !kubernetesSecretName.MatchString(field.SecretName) {
					problems = append(problems, fieldPrefix+": secretName must be a valid lowercase Kubernetes Secret name")
				}
				if field.FileName != "" && (path.Base(field.FileName) != field.FileName || field.FileName == "." || field.FileName == ".." || strings.ContainsAny(field.FileName, ":\r\n")) {
					problems = append(problems, fieldPrefix+": fileName must be a single file name without path traversal, ':' or newlines")
				}
			} else {
				if !strings.HasPrefix(field.MountPath, "/") || strings.ContainsAny(field.MountPath, ":\r\n") || path.Clean(field.MountPath) != field.MountPath {
					problems = append(problems, fieldPrefix+": mountPath must be an absolute, clean path without ':' or newlines")
				}
				if field.SecretName != "" || field.FileName != "" {
					problems = append(problems, fieldPrefix+": mountPath cannot be combined with secretName or fileName")
				}
			}
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return errors.New(strings.Join(problems, "\n"))
	}
	return nil
}
