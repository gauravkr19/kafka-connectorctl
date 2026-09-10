package workflow

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/example/connectorctl/internal/config"
	"github.com/example/connectorctl/internal/model"
	"github.com/example/connectorctl/internal/report"
	"github.com/example/connectorctl/internal/secret"
	"github.com/example/connectorctl/internal/yaml"
)

type MigrationPlan struct {
	APIVersion         string                       `yaml:"apiVersion"`
	NameReplacements   []Replacement                `yaml:"nameReplacements,omitempty"`
	ConfigReplacements []FieldReplacement           `yaml:"configReplacements,omitempty"`
	Overrides          map[string]ConnectorOverride `yaml:"overrides,omitempty"`
}

type Replacement struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}
type FieldReplacement struct {
	Fields []string `yaml:"fields"`
	From   string   `yaml:"from"`
	To     string   `yaml:"to"`
}
type ConnectorOverride struct {
	TargetName     string            `yaml:"targetName,omitempty"`
	ConnectProfile string            `yaml:"connectProfile,omitempty"`
	Set            map[string]string `yaml:"set,omitempty"`
	Remove         []string          `yaml:"remove,omitempty"`
}

type MigrateOptions struct {
	SourceCluster string
	SourceEnv     string
	TargetCluster string
	TargetEnv     string
	Selection     SelectionOptions
	PlanPath      string
	Concurrency   int
	DryRun        bool
	ChangeTicket  string
	ValidateLive  bool
	ServerDryRun  bool
	CheckLive     bool
}

func (s *Service) Migrate(ctx context.Context, opts MigrateOptions) (model.RunReport, string, error) {
	run := report.New("migrate", opts.TargetCluster, opts.TargetEnv, opts.DryRun)
	run.ChangeTicket = opts.ChangeTicket
	sourceCluster, sourceEnv, err := s.Bundle.ResolveTarget(opts.SourceCluster, opts.SourceEnv)
	if err != nil {
		return run, "", err
	}
	targetCluster, targetEnv, err := s.Bundle.ResolveTarget(opts.TargetCluster, opts.TargetEnv)
	if err != nil {
		return run, "", err
	}
	if s.Bundle.Policies.Create.RequireChangeTicket && strings.TrimSpace(opts.ChangeTicket) == "" {
		return run, "", fmt.Errorf("change ticket is required by create policy")
	}
	serverDryRun := opts.ServerDryRun || s.Bundle.Policies.Validation.ServerDryRun
	if !opts.DryRun && s.Bundle.Policies.Create.RequireDryRun && !serverDryRun {
		return run, "", fmt.Errorf("policy requires --server-dry-run before writing migrated connectors")
	}
	plan, err := loadMigrationPlan(opts.PlanPath)
	if err != nil {
		return run, "", err
	}
	sourceBase := sourceCluster.GitRoot + "/" + strings.ToLower(opts.SourceEnv)
	manifests, err := s.Repository.LoadConnectors(sourceBase)
	if err != nil {
		return run, "", err
	}
	byName := map[string]model.CFKConnector{}
	var sourceNames []string
	for _, manifest := range manifests {
		name := manifest.Connector.Spec.Name
		if name == "" {
			name = manifest.Connector.Metadata.Name
		}
		if _, duplicate := byName[name]; duplicate {
			return run, "", fmt.Errorf("source Git tree contains duplicate connector name %q", name)
		}
		byName[name] = manifest.Connector
		sourceNames = append(sourceNames, name)
	}
	if !opts.Selection.AllEnv && len(opts.Selection.Names) == 0 && opts.Selection.NamesFile == "" {
		opts.Selection.AllEnv = true
	}
	names, err := selectNames(sourceNames, sourceEnv.NameRegex, opts.Selection, true, s.Bundle.Policies.Defaults.BatchSize, s.Bundle.Policies.Adoption.MaxBatchSize)
	if err != nil {
		return run, "", err
	}
	mappings, err := configMappings(s, opts.TargetCluster, opts.TargetEnv)
	if err != nil {
		return run, "", err
	}
	profiles := allProfileNames(targetCluster)
	var installed map[string]map[string]struct{}
	if s.Bundle.Policies.Validation.VerifyPluginInstalled {
		installed, err = s.loadInstalledByProfile(ctx, strings.ToLower(opts.TargetCluster), profiles)
		if err != nil {
			return run, "", err
		}
	}
	var liveLocations map[string]string
	if opts.CheckLive {
		byProfile, err := s.loadConnectorNamesByProfile(ctx, strings.ToLower(opts.TargetCluster), profiles)
		if err != nil {
			return run, "", err
		}
		liveLocations, err = flattenProfileNames(byProfile)
		if err != nil {
			return run, "", err
		}
	}
	workers, err := s.validateConcurrency(opts.Concurrency)
	if err != nil {
		return run, "", err
	}
	clientFor := s.clientProvider(strings.ToLower(opts.TargetCluster))
	collector := &mutationCollector{}
	items := runConcurrent(ctx, names, workers, func(ctx context.Context, sourceName string) model.ItemResult {
		result := model.ItemResult{Name: sourceName, Action: "migrate"}
		sourceCR := byName[sourceName]
		raw := FullConnectConfig(sourceCR)
		targetName := defaultMigratedName(sourceName, opts.SourceEnv, opts.TargetEnv)
		targetName = applyNameReplacements(targetName, plan.NameReplacements)
		override := plan.Overrides[sourceName]
		if override.TargetName != "" {
			targetName = override.TargetName
		}
		if targetName == sourceName {
			result.Status = "failed"
			result.Error = "target connector name is unchanged; add a name replacement or override"
			return result
		}
		if !matchesEnv(targetName, targetEnv.NameRegex) {
			result.Status = "failed"
			result.Error = fmt.Sprintf("target name %q does not match target environment rule %q", targetName, targetEnv.NameRegex)
			return result
		}
		if opts.CheckLive {
			if profile, exists := liveLocations[targetName]; exists {
				result.Status = "failed"
				result.Error = fmt.Sprintf("target connector already exists in live Connect profile %q", profile)
				return result
			}
		}
		for _, rule := range plan.ConfigReplacements {
			for _, field := range rule.Fields {
				if value, ok := raw[field]; ok {
					raw[field] = strings.ReplaceAll(value, rule.From, rule.To)
				}
			}
		}
		for key, value := range override.Set {
			raw[key] = value
		}
		for _, key := range override.Remove {
			delete(raw, key)
		}
		entry, err := s.Registry.Lookup(raw["connector.class"])
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			return result
		}
		targetMapping := mappings.Connectors[targetName]
		if override.ConnectProfile != "" {
			targetMapping.ConnectProfile = override.ConnectProfile
		}
		detected := s.Registry.SensitiveKeys(entry, raw)
		sensitive, _ := sensitiveKeysWithMapping(detected, raw, targetMapping)
		sensitiveSet := make(map[string]struct{}, len(sensitive))
		for _, key := range sensitive {
			sensitiveSet[key] = struct{}{}
		}
		for key, value := range raw {
			if secret.IsExternalReference(value) {
				sensitiveSet[key] = struct{}{}
			}
		}
		sensitive = sensitive[:0]
		for key := range sensitiveSet {
			sensitive = append(sensitive, key)
		}
		sort.Strings(sensitive)
		for _, key := range sensitive {
			if _, ok := targetMapping.Fields[key]; !ok {
				result.Status = "failed"
				result.Error = fmt.Sprintf("target secret mapping for sensitive field %s is missing", key)
				return result
			}
			raw[key] = "__connectorctl_replace__"
		}
		localMappings := config.SecretMappingsFile{Connectors: map[string]config.ConnectorMapping{targetName: targetMapping}}
		built, err := s.buildConnector(ctx, targetName, raw, targetCluster, targetEnv, localMappings, buildOptions{
			ClusterName: strings.ToLower(opts.TargetCluster), LogicalEnv: strings.ToLower(opts.TargetEnv), Origin: "migrated-from-" + strings.ToLower(opts.SourceEnv), ChangeTicket: opts.ChangeTicket,
			ServerDryRun:       serverDryRun,
			ValidateLive:       opts.ValidateLive || s.Bundle.Policies.Validation.ValidateAgainstConnect,
			InstalledByProfile: installed, ClientForProfile: clientFor,
		})
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			return result
		}
		changed, previewPath, mutations, err := s.planPersist(built, opts.DryRun, false, run.RunID)
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			return result
		}
		collector.Add(mutations...)
		result.Name = targetName
		result.Plugin = built.Plugin.Alias
		result.ConnectProfile = built.ConnectProfile
		result.Path = built.Path
		result.PreviewPath = previewPath
		result.Changed = changed
		result.Warnings = built.Warnings
		result.Status = "succeeded"
		return result
	})
	report.Finalize(&run, items)
	applyErr := applyCollectedMutations(s.Repository, &run, collector)
	reportPath, writeErr := report.Write(s.ReportsDir, run)
	if writeErr != nil {
		return run, "", writeErr
	}
	if applyErr != nil {
		return run, reportPath, applyErr
	}
	if run.Failed > 0 {
		return run, reportPath, BatchError{Failed: run.Failed, Total: len(run.Items)}
	}
	return run, reportPath, nil
}

func loadMigrationPlan(path string) (MigrationPlan, error) {
	if strings.TrimSpace(path) == "" {
		return MigrationPlan{}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return MigrationPlan{}, err
	}
	defer f.Close()
	var plan MigrationPlan
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&plan); err != nil {
		return MigrationPlan{}, err
	}
	if plan.APIVersion != config.RegistryAPIVersion {
		return MigrationPlan{}, fmt.Errorf("migration plan apiVersion must be %q", config.RegistryAPIVersion)
	}
	for _, rule := range plan.ConfigReplacements {
		if len(rule.Fields) == 0 {
			return MigrationPlan{}, fmt.Errorf("every config replacement must list explicit fields")
		}
		if rule.From == "" {
			return MigrationPlan{}, fmt.Errorf("config replacement from value cannot be empty")
		}
	}
	for _, rule := range plan.NameReplacements {
		if rule.From == "" {
			return MigrationPlan{}, fmt.Errorf("name replacement from value cannot be empty")
		}
	}
	return plan, nil
}

func defaultMigratedName(name, sourceEnv, targetEnv string) string {
	re := regexp.MustCompile(`(?i)-` + regexp.QuoteMeta(sourceEnv) + `-`)
	location := re.FindStringIndex(name)
	if location == nil {
		return name
	}
	return name[:location[0]] + "-" + strings.ToLower(targetEnv) + "-" + name[location[1]:]
}
func applyNameReplacements(name string, replacements []Replacement) string {
	for _, replacement := range replacements {
		name = strings.ReplaceAll(name, replacement.From, replacement.To)
	}
	return name
}
