package workflow

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/example/connectorctl/internal/config"
	"github.com/example/connectorctl/internal/connect"
	"github.com/example/connectorctl/internal/model"
	"github.com/example/connectorctl/internal/registry"
	"github.com/example/connectorctl/internal/repository"
	"github.com/example/connectorctl/internal/transform"
)

type clientProvider func(profile string) (*connect.Client, error)

type buildOptions struct {
	ClusterName           string
	LogicalEnv            string
	Origin                string
	ChangeTicket          string
	ForcedConnectProfile  string
	RejectProfileOverride bool
	ServerDryRun          bool
	ValidateLive          bool
	InstalledByProfile    map[string]map[string]struct{}
	ClientForProfile      clientProvider
}

type builtConnector struct {
	CR             model.CFKConnector
	YAML           []byte
	Plugin         registry.Entry
	ConnectProfile string
	SensitiveKeys  []string
	Path           string
	Warnings       []string
}

func (s *Service) buildConnector(ctx context.Context, name string, raw map[string]string, cluster config.Cluster, env config.Environment, mappings config.SecretMappingsFile, opts buildOptions) (builtConnector, error) {
	className := strings.TrimSpace(raw["connector.class"])
	entry, err := s.Registry.Lookup(className)
	if err != nil {
		return builtConnector{}, err
	}
	if err := s.Registry.Validate(entry, raw); err != nil {
		return builtConnector{}, err
	}
	mapping := mappings.Connectors[name]
	profile, err := resolveConnectProfile(entry, env, mapping, opts.ForcedConnectProfile, opts.RejectProfileOverride)
	if err != nil {
		return builtConnector{}, fmt.Errorf("connector %s: %w", name, err)
	}
	if _, ok := cluster.ConnectProfiles[profile]; !ok {
		return builtConnector{}, fmt.Errorf("connector %s resolves to unknown connect profile %q", name, profile)
	}
	if opts.InstalledByProfile != nil {
		installed, ok := opts.InstalledByProfile[profile]
		if !ok {
			return builtConnector{}, fmt.Errorf("installed plugin inventory was not loaded for connect profile %q", profile)
		}
		if !pluginInstalled(className, installed) {
			return builtConnector{}, fmt.Errorf("connector class %q is not installed on connect profile %q", className, profile)
		}
	}

	sensitiveKeys, mappingWarnings := sensitiveKeysWithMapping(s.Registry.SensitiveKeys(entry, raw), raw, mapping)
	sanitized, refs, err := s.Secrets.Resolve(name, raw, sensitiveKeys, mapping)
	if err != nil {
		return builtConnector{}, err
	}
	normalized, err := transform.Normalize(name, opts.ClusterName, opts.LogicalEnv, entry.Alias, opts.Origin, opts.ChangeTicket, sanitized, profile)
	if err != nil {
		return builtConnector{}, err
	}
	cr, err := s.Renderer.Build(normalized, cluster)
	if err != nil {
		return builtConnector{}, err
	}
	yamlData, err := s.Renderer.Marshal(cr)
	if err != nil {
		return builtConnector{}, err
	}
	if opts.ValidateLive {
		if opts.ClientForProfile == nil {
			return builtConnector{}, fmt.Errorf("live validation requested without a client provider")
		}
		client, err := opts.ClientForProfile(profile)
		if err != nil {
			return builtConnector{}, err
		}

		validationConfig := FullConnectConfig(cr)
		validationConfig["name"] = cr.Metadata.Name

		response, err := client.ValidateConfig(ctx, className, validationConfig)

		// response, err := client.ValidateConfig(ctx, className, FullConnectConfig(cr))
		// if err != nil {
		// 	return builtConnector{}, err
		// }
		if response.ErrorCount > 0 {
			keys := validationErrorKeys(response)
			if len(keys) > 0 {
				return builtConnector{}, fmt.Errorf("live plugin validation on profile %q reported %d errors for configuration keys: %s", profile, response.ErrorCount, strings.Join(keys, ", "))
			}
			return builtConnector{}, fmt.Errorf("live plugin validation on profile %q reported %d errors (messages suppressed)", profile, response.ErrorCount)
		}
	}
	if opts.ServerDryRun {
		if err := s.serverDryRun(ctx, yamlData); err != nil {
			return builtConnector{}, err
		}
	}
	path := s.Repository.ConnectorPath(cluster.GitRoot, opts.LogicalEnv, entry.Alias, cr.Metadata.Name)
	warnings := append([]string{}, mappingWarnings...)
	for _, ref := range refs {
		warnings = append(warnings, "replaced "+ref.ConfigKey+" with external reference")
	}
	return builtConnector{CR: cr, YAML: yamlData, Plugin: entry, ConnectProfile: profile, SensitiveKeys: sensitiveKeys, Path: path, Warnings: warnings}, nil
}

func validationErrorKeys(response model.ConfigValidationResponse) []string {
	set := map[string]struct{}{}
	for _, item := range response.Configs {
		errorsValue, exists := item.Value["errors"]
		if !exists {
			continue
		}
		hasErrors := false
		switch values := errorsValue.(type) {
		case []any:
			hasErrors = len(values) > 0
		case []string:
			hasErrors = len(values) > 0
		case string:
			hasErrors = strings.TrimSpace(values) != ""
		default:
			hasErrors = values != nil
		}
		if !hasErrors {
			continue
		}
		name, _ := item.Value["name"].(string)
		if strings.TrimSpace(name) == "" {
			name, _ = item.Definition["name"].(string)
		}
		if strings.TrimSpace(name) != "" {
			set[name] = struct{}{}
		}
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sensitiveKeysWithMapping(detected []string, raw map[string]string, mapping config.ConnectorMapping) ([]string, []string) {
	set := make(map[string]struct{}, len(detected)+len(mapping.Fields))
	for _, key := range detected {
		set[key] = struct{}{}
	}
	var warnings []string
	for key := range mapping.Fields {
		if _, exists := raw[key]; !exists {
			warnings = append(warnings, "secret mapping field "+key+" is not present in the connector configuration")
			continue
		}
		set[key] = struct{}{}
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	sort.Strings(warnings)
	return keys, warnings
}

func resolveConnectProfile(entry registry.Entry, env config.Environment, mapping config.ConnectorMapping, forced string, rejectOverride bool) (string, error) {
	forced = strings.ToLower(strings.TrimSpace(forced))
	mapped := strings.ToLower(strings.TrimSpace(mapping.ConnectProfile))
	if forced != "" {
		if rejectOverride && mapped != "" && mapped != forced {
			return "", fmt.Errorf("secret mapping requests connect profile %q but operation must preserve source profile %q", mapped, forced)
		}
		return forced, nil
	}
	if mapped != "" {
		return mapped, nil
	}
	if profile := strings.ToLower(strings.TrimSpace(entry.Rule.DefaultConnectProfile)); profile != "" {
		return profile, nil
	}
	if profile := strings.ToLower(strings.TrimSpace(env.DefaultConnectProfile)); profile != "" {
		return profile, nil
	}
	return "default", nil
}

func (s *Service) planPersist(b builtConnector, dryRun, overwrite bool, runID string) (bool, string, []repository.Mutation, error) {
	changed, err := s.Repository.PlanWrite(b.Path, b.YAML, overwrite)
	if err != nil {
		return false, "", nil, err
	}
	previewPath := ""
	if dryRun && s.RenderDir != "" {
		relative, err := filepath.Rel(s.Repository.Root, b.Path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return false, "", nil, fmt.Errorf("cannot derive preview path for %s", b.Path)
		}
		previewPath = filepath.Join(s.RenderDir, runID, relative)
		if _, err := s.Repository.WriteAtomic(previewPath, b.YAML, true); err != nil {
			return false, "", nil, fmt.Errorf("write dry-run preview: %w", err)
		}
	}
	if dryRun || !changed {
		return changed, previewPath, nil, nil
	}
	return true, previewPath, []repository.Mutation{{Type: repository.MutationWrite, Path: b.Path, Content: b.YAML, Overwrite: overwrite}}, nil
}

func (s *Service) serverDryRun(ctx context.Context, yamlData []byte) error {
	tmp, err := os.CreateTemp("", "connectorctl-*.yaml")
	if err != nil {
		return err
	}
	path := tmp.Name()
	defer os.Remove(path)
	if _, err := tmp.Write(yamlData); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, s.KubectlBin, "apply", "--dry-run=server", "-f", filepath.Clean(path))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("server dry-run failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func FullConnectConfig(cr model.CFKConnector) map[string]string {
	out := make(map[string]string, len(cr.Spec.Configs)+2)
	for key, value := range cr.Spec.Configs {
		out[key] = value
	}
	out["connector.class"] = cr.Spec.Class
	out["tasks.max"] = fmt.Sprintf("%d", cr.Spec.TaskMax)
	return out
}

func pluginInstalled(className string, installed map[string]struct{}) bool {
	className = strings.TrimSpace(className)
	if _, ok := installed[className]; ok {
		return true
	}
	// Kafka Connect accepts unambiguous simple aliases in some deployments. If
	// the desired configuration uses a fully qualified class, never satisfy it
	// through an unrelated plugin with the same short name.
	if strings.Contains(className, ".") {
		return false
	}
	for installedClass := range installed {
		short := installedClass
		if i := strings.LastIndex(installedClass, "."); i >= 0 {
			short = installedClass[i+1:]
		}
		if short == className {
			return true
		}
	}
	return false
}

func installedSet(plugins []model.PluginInfo) map[string]struct{} {
	set := map[string]struct{}{}
	for _, plugin := range plugins {
		if className := strings.TrimSpace(plugin.Class); className != "" {
			set[className] = struct{}{}
		}
	}
	return set
}
