package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ValidateMappingFiles validates every mapping file that exists under
// config/mappings and rejects files placed under an unknown cluster or logical
// environment. Missing mapping files are allowed for environments whose
// connectors do not require guided secret replacement.
func (b *Bundle) ValidateMappingFiles() error {
	root := filepath.Join(b.ConfigDir, "mappings")
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s must be a directory", root)
	}
	var problems []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if filepath.Ext(entry.Name()) != ".yaml" {
			problems = append(problems, fmt.Sprintf("mappings/%s: only lowercase .yaml mapping files are supported", filepath.ToSlash(rel)))
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 2 {
			problems = append(problems, fmt.Sprintf("mappings/%s: expected path mappings/<cluster>/<environment>.yaml", filepath.ToSlash(rel)))
			return nil
		}
		clusterName := strings.ToLower(parts[0])
		envName := strings.ToLower(strings.TrimSuffix(parts[1], filepath.Ext(parts[1])))
		if parts[0] != clusterName || parts[1] != envName+".yaml" {
			problems = append(problems, fmt.Sprintf("mappings/%s: cluster directory and environment filename must be lowercase", filepath.ToSlash(rel)))
			return nil
		}
		cluster, ok := b.Platforms.Clusters[clusterName]
		if !ok {
			problems = append(problems, fmt.Sprintf("mappings/%s: unknown physical cluster %q", filepath.ToSlash(rel), clusterName))
			return nil
		}
		env, ok := cluster.LogicalEnvironments[envName]
		if !ok {
			problems = append(problems, fmt.Sprintf("mappings/%s: environment %q is not mapped to cluster %q", filepath.ToSlash(rel), envName, clusterName))
			return nil
		}
		var mappings SecretMappingsFile
		if err := decodeStrict(path, &mappings); err != nil {
			problems = append(problems, fmt.Sprintf("mappings/%s: %v", filepath.ToSlash(rel), err))
			return nil
		}
		if err := ValidateSecretMappings(mappings, b.SecretProfiles); err != nil {
			problems = append(problems, fmt.Sprintf("mappings/%s: %v", filepath.ToSlash(rel), err))
		}
		re, err := regexp.Compile(env.NameRegex)
		if err != nil {
			return err // Base bundle validation should already have caught this.
		}
		for connectorName, mapping := range mappings.Connectors {
			if !re.MatchString(connectorName) {
				problems = append(problems, fmt.Sprintf("mappings/%s: connector %q does not match environment rule %q", filepath.ToSlash(rel), connectorName, env.NameRegex))
			}
			if mapping.ConnectProfile != "" {
				if _, ok := cluster.ConnectProfiles[mapping.ConnectProfile]; !ok {
					problems = append(problems, fmt.Sprintf("mappings/%s: connector %q references unknown connect profile %q", filepath.ToSlash(rel), connectorName, mapping.ConnectProfile))
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("mapping validation failed:\n%s", strings.Join(problems, "\n"))
	}
	return nil
}
