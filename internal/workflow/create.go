package workflow

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/example/connectorctl/internal/model"
	"github.com/example/connectorctl/internal/report"
	"github.com/example/connectorctl/internal/repository"
)

type CreateOptions struct {
	Cluster      string
	LogicalEnv   string
	InputPath    string
	Name         string
	Concurrency  int
	BatchSize    int
	BatchOffset  int
	UnsafeAll    bool
	DryRun       bool
	ChangeTicket string
	ValidateLive bool
	ServerDryRun bool
	CheckLive    bool
}

func (s *Service) Create(ctx context.Context, opts CreateOptions) (model.RunReport, string, error) {
	return s.createOrUpdate(ctx, "create", opts, false)
}

func (s *Service) Update(ctx context.Context, opts CreateOptions) (model.RunReport, string, error) {
	return s.createOrUpdate(ctx, "update", opts, true)
}

// createOrUpdate loads one JSON config or a directory of JSON configs,
// validates each connector, applies batch limits, and processes them concurrently.
func (s *Service) createOrUpdate(ctx context.Context, action string, opts CreateOptions, update bool) (model.RunReport, string, error) {
	run := report.New(action, opts.Cluster, opts.LogicalEnv, opts.DryRun)
	run.ChangeTicket = opts.ChangeTicket
	cluster, env, err := s.Bundle.ResolveTarget(opts.Cluster, opts.LogicalEnv)
	if err != nil {
		return run, "", err
	}
	policy := s.Bundle.Policies.Create
	if update {
		policy = s.Bundle.Policies.Update
	}
	if policy.RequireChangeTicket && strings.TrimSpace(opts.ChangeTicket) == "" {
		return run, "", fmt.Errorf("change ticket is required by policy")
	}
	serverDryRun := opts.ServerDryRun || s.Bundle.Policies.Validation.ServerDryRun
	if !opts.DryRun && policy.RequireDryRun && !serverDryRun {
		return run, "", fmt.Errorf("policy requires --server-dry-run before writing %s changes", action)
	}
	inputs, err := repository.LoadJSONInputs(opts.InputPath, opts.Name)
	if err != nil {
		return run, "", err
	}
	byName := map[string]repository.ParsedConfig{}
	var names []string
	for _, input := range inputs {
		if _, exists := byName[input.Name]; exists {
			return run, "", fmt.Errorf("duplicate connector name %q in input", input.Name)
		}
		byName[input.Name] = input
		names = append(names, input.Name)
	}
	sort.Strings(names)
	names, err = applyBatchLimits(names, opts.BatchSize, opts.BatchOffset, opts.UnsafeAll, s.Bundle.Policies.Adoption.AllowWholeEnvironment, s.Bundle.Policies.Adoption.MaxBatchSize)
	if err != nil {
		return run, "", err
	}
	mappings, err := configMappings(s, opts.Cluster, opts.LogicalEnv)
	if err != nil {
		return run, "", err
	}
	profiles := allProfileNames(cluster)
	needLive := opts.ValidateLive || opts.CheckLive || s.Bundle.Policies.Validation.VerifyPluginInstalled || s.Bundle.Policies.Validation.ValidateAgainstConnect
	var installed map[string]map[string]struct{}
	var liveLocations map[string]string
	if needLive && s.Bundle.Policies.Validation.VerifyPluginInstalled {
		installed, err = s.loadInstalledByProfile(ctx, strings.ToLower(opts.Cluster), profiles)
		if err != nil {
			return run, "", err
		}
	}
	if opts.CheckLive {
		byProfile, err := s.loadConnectorNamesByProfile(ctx, strings.ToLower(opts.Cluster), profiles)
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
	clientFor := s.clientProvider(strings.ToLower(opts.Cluster))
	collector := &mutationCollector{}
	items := runConcurrent(ctx, names, workers, func(ctx context.Context, name string) model.ItemResult {
		input := byName[name]
		result := model.ItemResult{Name: name, Action: action, Warnings: append([]string{}, input.Warnings...)}
		if !matchesEnv(name, env.NameRegex) {
			result.Status = "failed"
			result.Error = fmt.Sprintf("connector name does not match environment rule %q", env.NameRegex)
			return result
		}
		base := cluster.GitRoot + "/" + strings.ToLower(opts.LogicalEnv)
		var existing repository.Manifest
		forcedProfile := ""
		if update {
			var err error
			existing, err = s.Repository.FindByConnectorName(base, name)
			if err != nil {
				result.Status = "failed"
				result.Error = err.Error()
				return result
			}
			forcedProfile, err = profileFromCR(existing.Connector, cluster)
			if err != nil {
				result.Status = "failed"
				result.Error = err.Error()
				return result
			}
		}
		built, err := s.buildConnector(ctx, name, input.Config, cluster, env, mappings, buildOptions{
			ClusterName: strings.ToLower(opts.Cluster), LogicalEnv: strings.ToLower(opts.LogicalEnv), Origin: action, ChangeTicket: opts.ChangeTicket,
			ForcedConnectProfile: forcedProfile, RejectProfileOverride: update,
			ServerDryRun:       serverDryRun,
			ValidateLive:       opts.ValidateLive || s.Bundle.Policies.Validation.ValidateAgainstConnect,
			InstalledByProfile: installed, ClientForProfile: clientFor,
		})
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			return result
		}
		if opts.CheckLive {
			liveProfile, liveExists := liveLocations[name]
			if !update && liveExists {
				result.Status = "failed"
				result.Error = fmt.Sprintf("connector already exists in live Connect profile %q; use adopt", liveProfile)
				return result
			}
			if update && !liveExists {
				result.Status = "failed"
				result.Error = "connector does not exist in any live Connect profile"
				return result
			}
			if update && liveProfile != built.ConnectProfile {
				result.Status = "failed"
				result.Error = fmt.Sprintf("managed CR resolves to profile %q but live connector is on profile %q", built.ConnectProfile, liveProfile)
				return result
			}
		}
		overwrite := update && existing.Path == built.Path
		changed, previewPath, mutations, err := s.planPersist(built, opts.DryRun, overwrite, run.RunID)
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			return result
		}
		if update && existing.Path != built.Path {
			if err := s.Repository.PlanRemove(existing.Path); err != nil {
				result.Status = "failed"
				result.Error = "cannot plan manifest path move: " + err.Error()
				return result
			}
			changed = true
			if !opts.DryRun {
				mutations = append(mutations, repository.Mutation{Type: repository.MutationRemove, Path: existing.Path})
			}
		}
		collector.Add(mutations...)
		result.Plugin = built.Plugin.Alias
		result.ConnectProfile = built.ConnectProfile
		result.Path = built.Path
		result.PreviewPath = previewPath
		result.Changed = changed
		result.Warnings = append(result.Warnings, built.Warnings...)
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
