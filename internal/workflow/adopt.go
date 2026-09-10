package workflow

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/example/connectorctl/internal/model"
	"github.com/example/connectorctl/internal/report"
	"github.com/example/connectorctl/internal/transform"
)

type AdoptOptions struct {
	Cluster       string
	LogicalEnv    string
	SourceProfile string
	Selection     SelectionOptions
	Concurrency   int
	DryRun        bool
	ChangeTicket  string
	ValidateLive  bool
	ServerDryRun  bool
}

func (s *Service) Adopt(ctx context.Context, opts AdoptOptions) (model.RunReport, string, error) {
	run := report.New("adopt", opts.Cluster, opts.LogicalEnv, opts.DryRun)
	run.ChangeTicket = opts.ChangeTicket

	if err := validateAdoptSelection(opts.Selection); err != nil {
		return run, "", err
	}
	cluster, env, err := s.Bundle.ResolveTarget(opts.Cluster, opts.LogicalEnv)
	if err != nil {
		return run, "", err
	}
	locations, err := s.discoverLiveConnectors(ctx, strings.ToLower(opts.Cluster), cluster, env, opts.SourceProfile)
	if err != nil {
		return run, "", err
	}
	base := cluster.GitRoot + "/" + strings.ToLower(opts.LogicalEnv)
	manifests, err := s.Repository.LoadConnectors(base)
	if err != nil {
		return run, "", err
	}
	managed := map[string]struct{}{}
	for _, manifest := range manifests {
		name := manifest.Connector.Spec.Name
		if name == "" {
			name = manifest.Connector.Metadata.Name
		}
		managed[name] = struct{}{}
	}
	allLive := make([]string, 0, len(locations))
	for name := range locations {
		if opts.Selection.AllEnv {
			if _, exists := managed[name]; exists {
				continue
			}
		}
		allLive = append(allLive, name)
	}
	sort.Strings(allLive)
	names, err := selectNames(allLive, env.NameRegex, opts.Selection, s.Bundle.Policies.Adoption.AllowWholeEnvironment, s.Bundle.Policies.Defaults.BatchSize, s.Bundle.Policies.Adoption.MaxBatchSize)
	if err != nil {
		if opts.Selection.AllEnv && len(allLive) == 0 {
			return run, "", fmt.Errorf("no pending connectors remain for environment %s", strings.ToUpper(opts.LogicalEnv))
		}
		return run, "", err
	}
	mappings, err := configMappings(s, opts.Cluster, opts.LogicalEnv)
	if err != nil {
		return run, "", err
	}
	profiles, err := discoveryProfiles(cluster, env, opts.SourceProfile)
	if err != nil {
		return run, "", err
	}
	var installed map[string]map[string]struct{}
	if s.Bundle.Policies.Validation.VerifyPluginInstalled {
		installed, err = s.loadInstalledByProfile(ctx, strings.ToLower(opts.Cluster), profiles)
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

	// connector names go thru env rule, sorting, applyBatchLimits(), and finally here - Adopt processes the list concurrently.
	items := runConcurrent(ctx, names, workers, func(ctx context.Context, name string) model.ItemResult {
		result := model.ItemResult{Name: name, Action: "adopt"}
		if _, exists := managed[name]; exists {
			result.Status = "skipped"
			result.Warnings = []string{"connector is already represented in Git; use update for managed connectors"}
			return result
		}
		sourceProfile, ok := locations[name]
		if !ok {
			result.Status = "failed"
			result.Error = "connector was not found in the configured adoption-source Connect profiles"
			return result
		}
		result.ConnectProfile = sourceProfile
		client, err := clientFor(sourceProfile)
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			return result
		}
		liveConfig, err := client.GetConfig(ctx, name)
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			return result
		}
		status, statusErr := client.GetStatus(ctx, name)
		built, err := s.buildConnector(ctx, name, liveConfig, cluster, env, mappings, buildOptions{
			ClusterName: strings.ToLower(opts.Cluster), LogicalEnv: strings.ToLower(opts.LogicalEnv), Origin: "adopted", ChangeTicket: opts.ChangeTicket,
			ForcedConnectProfile: sourceProfile, RejectProfileOverride: true,
			ServerDryRun:       opts.ServerDryRun || s.Bundle.Policies.Validation.ServerDryRun,
			ValidateLive:       opts.ValidateLive || s.Bundle.Policies.Validation.ValidateAgainstConnect,
			InstalledByProfile: installed, ClientForProfile: clientFor,
		})
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			return result
		}
		if s.Bundle.Policies.Adoption.RequireFunctionalDiff {
			diffs := transform.AdoptionDiff(liveConfig, FullConnectConfig(built.CR), built.SensitiveKeys)
			if len(diffs) > 0 {
				result.Status = "failed"
				result.Error = fmt.Sprintf("functional adoption diff found %d mismatch(es); inspect dry-run output and source config", len(diffs))
				return result
			}
		}
		changed, previewPath, mutations, err := s.planPersist(built, opts.DryRun, false, run.RunID)
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			return result
		}
		result.Plugin = built.Plugin.Alias
		result.ConnectProfile = built.ConnectProfile
		collector.Add(mutations...)
		result.Path = built.Path
		result.PreviewPath = previewPath
		result.Changed = changed
		result.Warnings = append(result.Warnings, built.Warnings...)
		if statusErr != nil {
			result.Warnings = append(result.Warnings, "status check failed: "+statusErr.Error())
		} else {
			result.TaskStates = statusStates(status)
			if !strings.EqualFold(status.Connector.State, "RUNNING") {
				result.Warnings = append(result.Warnings, "connector state before adoption is "+status.Connector.State)
			}
		}
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

func statusStates(status model.ConnectorStatus) []string {
	states := []string{"connector=" + status.Connector.State}
	for _, task := range status.Tasks {
		states = append(states, fmt.Sprintf("task-%d=%s", task.ID, task.State))
	}
	return states
}
