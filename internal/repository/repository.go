package repository

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/example/connectorctl/internal/model"
	"github.com/example/connectorctl/internal/yaml"
)

type Repository struct{ Root string }

func New(root string) *Repository { return &Repository{Root: root} }

func (r *Repository) ConnectorPath(clusterGitRoot, logicalEnv, pluginAlias, crName string) string {
	return filepath.Join(r.Root, clusterGitRoot, strings.ToLower(logicalEnv), strings.ToLower(pluginAlias), crName+".yaml")
}

func (r *Repository) WriteAtomic(path string, content []byte, overwrite bool) (bool, error) {
	if current, err := os.ReadFile(path); err == nil {
		if string(current) == string(content) {
			return false, nil
		}
		if !overwrite {
			return false, fmt.Errorf("file already exists and overwrite is disabled: %s", path)
		}
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".connectorctl-*.tmp")
	if err != nil {
		return false, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repository) Remove(path string) error {
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("connector manifest does not exist: %s", path)
		}
		return err
	}
	return nil
}

func (r *Repository) LoadConnectors(base string) ([]Manifest, error) {
	root := filepath.Join(r.Root, base)
	var manifests []Manifest
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if entry.IsDir() || !(strings.HasSuffix(entry.Name(), ".yaml") || strings.HasSuffix(entry.Name(), ".yml")) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var cr model.CFKConnector
		decoder := yaml.NewDecoder(strings.NewReader(string(data)))
		decoder.KnownFields(true)
		if err := decoder.Decode(&cr); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("parse trailing YAML in %s: %w", path, err)
		} else if err == nil && extra != nil {
			return fmt.Errorf("multiple YAML documents are not supported in %s", path)
		}
		if cr.Kind != "Connector" {
			return fmt.Errorf("unexpected Kubernetes kind %q in connector source tree: %s", cr.Kind, path)
		}
		manifests = append(manifests, Manifest{Path: path, Connector: cr})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(manifests, func(i, j int) bool { return manifests[i].Path < manifests[j].Path })
	return manifests, nil
}

func (r *Repository) FindByConnectorName(base, connectorName string) (Manifest, error) {
	manifests, err := r.LoadConnectors(base)
	if err != nil {
		return Manifest{}, err
	}
	for _, manifest := range manifests {
		name := manifest.Connector.Spec.Name
		if name == "" {
			name = manifest.Connector.Metadata.Name
		}
		if name == connectorName {
			return manifest, nil
		}
	}
	return Manifest{}, fmt.Errorf("connector %q was not found under %s", connectorName, base)
}

type Manifest struct {
	Path      string
	Connector model.CFKConnector
}
