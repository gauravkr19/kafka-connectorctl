package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/example/connectorctl/internal/yaml"
)

func LoadBundle(dir string) (*Bundle, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("config directory is required")
	}
	b := &Bundle{ConfigDir: dir}
	if err := decodeStrict(filepath.Join(dir, "platforms.yaml"), &b.Platforms); err != nil {
		return nil, fmt.Errorf("load platforms: %w", err)
	}
	if err := decodeStrict(filepath.Join(dir, "plugins.yaml"), &b.Plugins); err != nil {
		return nil, fmt.Errorf("load plugins: %w", err)
	}
	if err := decodeStrict(filepath.Join(dir, "policies.yaml"), &b.Policies); err != nil {
		return nil, fmt.Errorf("load policies: %w", err)
	}
	if err := decodeStrict(filepath.Join(dir, "secret-profiles.yaml"), &b.SecretProfiles); err != nil {
		return nil, fmt.Errorf("load secret profiles: %w", err)
	}
	if err := b.Validate(); err != nil {
		return nil, err
	}
	if err := b.ValidateMappingFiles(); err != nil {
		return nil, err
	}
	return b, nil
}

func LoadSecretMappings(configDir, cluster, logicalEnv string) (SecretMappingsFile, error) {
	path := filepath.Join(configDir, "mappings", strings.ToLower(cluster), strings.ToLower(logicalEnv)+".yaml")
	var mappings SecretMappingsFile
	if err := decodeStrict(path, &mappings); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			mappings.APIVersion = RegistryAPIVersion
			mappings.Connectors = map[string]ConnectorMapping{}
			return mappings, nil
		}
		return mappings, fmt.Errorf("load secret mappings %s: %w", path, err)
	}
	if mappings.Connectors == nil {
		mappings.Connectors = map[string]ConnectorMapping{}
	}
	return mappings, nil
}

func decodeStrict(path string, target any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != nil && !errors.Is(err, io.EOF) {
		return err
	} else if err == nil && extra != nil {
		return errors.New("multiple YAML documents are not supported")
	}
	return nil
}
