package workflow

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/example/connectorctl/internal/secret"
)

type RepositoryValidation struct {
	FilesChecked int      `json:"filesChecked" yaml:"filesChecked"`
	Clusters     int      `json:"clusters" yaml:"clusters"`
	Environments int      `json:"environments" yaml:"environments"`
	Errors       []string `json:"errors,omitempty" yaml:"errors,omitempty"`
}

func (s *Service) ValidateRepository() (RepositoryValidation, error) {
	result := RepositoryValidation{}
	metadataOwners := map[string]string{}
	for clusterName, cluster := range s.Bundle.Platforms.Clusters {
		if !cluster.Enabled {
			continue
		}
		result.Clusters++
		connectorOwners := map[string]string{}
		for envName, env := range cluster.LogicalEnvironments {
			if env.Enabled != nil && !*env.Enabled {
				continue
			}
			result.Environments++
			mappings, mappingErr := configMappings(s, clusterName, envName)
			if mappingErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s/%s mappings: %v", clusterName, envName, mappingErr))
				continue
			}
			base := cluster.GitRoot + "/" + envName
			manifests, err := s.Repository.LoadConnectors(base)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s/%s: %v", clusterName, envName, err))
				continue
			}
			for _, manifest := range manifests {
				result.FilesChecked++
				cr := manifest.Connector
				prefix := manifest.Path
				if cr.APIVersion != s.Renderer.APIVersion || cr.Kind != s.Renderer.Kind {
					result.Errors = append(result.Errors, fmt.Sprintf("%s: expected %s %s", prefix, s.Renderer.APIVersion, s.Renderer.Kind))
				}
				if cr.Metadata.Namespace != cluster.Namespace {
					result.Errors = append(result.Errors, fmt.Sprintf("%s: namespace %q does not match cluster namespace %q", prefix, cr.Metadata.Namespace, cluster.Namespace))
				}
				name := cr.Spec.Name
				if name == "" {
					name = cr.Metadata.Name
				}
				if !matchesEnv(name, env.NameRegex) {
					result.Errors = append(result.Errors, fmt.Sprintf("%s: connector name %q does not match environment %s", prefix, name, envName))
				}
				ownerKey := clusterName + "/" + name
				if previous, exists := connectorOwners[ownerKey]; exists {
					result.Errors = append(result.Errors, fmt.Sprintf("%s: duplicate connector spec.name %q also found at %s", prefix, name, previous))
				} else {
					connectorOwners[ownerKey] = prefix
				}
				metadataKey := cluster.Namespace + "/" + cr.Metadata.Name
				if previous, exists := metadataOwners[metadataKey]; exists {
					result.Errors = append(result.Errors, fmt.Sprintf("%s: duplicate Kubernetes metadata.name %q also found at %s", prefix, cr.Metadata.Name, previous))
				} else {
					metadataOwners[metadataKey] = prefix
				}
				entry, err := s.Registry.Lookup(cr.Spec.Class)
				if err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", prefix, err))
					continue
				}
				if label := cr.Metadata.Labels["connectorctl.io/connector-plugin"]; label != entry.Alias {
					result.Errors = append(result.Errors, fmt.Sprintf("%s: connector-plugin label %q should be %q", prefix, label, entry.Alias))
				}
				if label := cr.Metadata.Labels["connectorctl.io/logical-env"]; label != envName {
					result.Errors = append(result.Errors, fmt.Sprintf("%s: logical-env label %q should be %q", prefix, label, envName))
				}
				if label := cr.Metadata.Labels["connectorctl.io/physical-cluster"]; label != clusterName {
					result.Errors = append(result.Errors, fmt.Sprintf("%s: physical-cluster label %q should be %q", prefix, label, clusterName))
				}
				profile, err := profileFromCR(cr, cluster)
				if err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", prefix, err))
				} else if label := cr.Metadata.Labels["connectorctl.io/connect-profile"]; label != profile {
					result.Errors = append(result.Errors, fmt.Sprintf("%s: connect-profile label %q should be %q", prefix, label, profile))
				}
				expectedPath := s.Repository.ConnectorPath(cluster.GitRoot, envName, entry.Alias, cr.Metadata.Name)
				if filepath.Clean(expectedPath) != filepath.Clean(manifest.Path) {
					result.Errors = append(result.Errors, fmt.Sprintf("%s: expected class-organized path %s", prefix, expectedPath))
				}
				full := FullConnectConfig(cr)
				if err := s.Registry.Validate(entry, full); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", prefix, err))
				}
				mapping := mappings.Connectors[name]
				sensitiveKeys, mappingWarnings := sensitiveKeysWithMapping(s.Registry.SensitiveKeys(entry, full), full, mapping)
				for _, warning := range mappingWarnings {
					result.Errors = append(result.Errors, fmt.Sprintf("%s: %s", prefix, warning))
				}
				for _, key := range sensitiveKeys {
					if !secret.IsExternalReference(full[key]) {
						result.Errors = append(result.Errors, fmt.Sprintf("%s: sensitive configuration key %q is not an external reference", prefix, key))
					}
				}
			}
		}
	}
	sort.Strings(result.Errors)
	if len(result.Errors) > 0 {
		return result, fmt.Errorf("repository validation failed with %d error(s)", len(result.Errors))
	}
	return result, nil
}
