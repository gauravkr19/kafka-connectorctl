package workflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/example/connectorctl/internal/model"
	"github.com/example/connectorctl/internal/report"
	"github.com/example/connectorctl/internal/repository"
)

type DeleteOptions struct {
	Cluster      string
	LogicalEnv   string
	Selection    SelectionOptions
	Concurrency  int
	DryRun       bool
	ChangeTicket string
	Confirm      bool
}

func (s *Service) Delete(ctx context.Context, opts DeleteOptions) (model.RunReport, string, error) {
	run := report.New("delete", opts.Cluster, opts.LogicalEnv, opts.DryRun)
	run.ChangeTicket = opts.ChangeTicket
	cluster, env, err := s.Bundle.ResolveTarget(opts.Cluster, opts.LogicalEnv)
	if err != nil {
		return run, "", err
	}
	if s.Bundle.Policies.Delete.RequireChangeTicket && strings.TrimSpace(opts.ChangeTicket) == "" {
		return run, "", fmt.Errorf("change ticket is required by delete policy")
	}
	if !opts.DryRun && s.Bundle.Policies.Delete.RequireManualApproval && !opts.Confirm {
		return run, "", fmt.Errorf("delete requires --confirm")
	}
	base := cluster.GitRoot + "/" + strings.ToLower(opts.LogicalEnv)
	manifests, err := s.Repository.LoadConnectors(base)
	if err != nil {
		return run, "", err
	}
	byName := map[string]string{}
	profiles := map[string]string{}
	var repoNames []string
	for _, manifest := range manifests {
		name := manifest.Connector.Spec.Name
		if name == "" {
			name = manifest.Connector.Metadata.Name
		}
		if _, duplicate := byName[name]; duplicate {
			return run, "", fmt.Errorf("Git contains duplicate connector name %q", name)
		}
		profile, err := profileFromCR(manifest.Connector, cluster)
		if err != nil {
			return run, "", err
		}
		byName[name] = manifest.Path
		profiles[name] = profile
		repoNames = append(repoNames, name)
	}
	names, err := selectNames(repoNames, env.NameRegex, opts.Selection, s.Bundle.Policies.Adoption.AllowWholeEnvironment, s.Bundle.Policies.Defaults.BatchSize, s.Bundle.Policies.Adoption.MaxBatchSize)
	if err != nil {
		return run, "", err
	}
	workers, err := s.validateConcurrency(opts.Concurrency)
	if err != nil {
		return run, "", err
	}
	collector := &mutationCollector{}
	items := runConcurrent(ctx, names, workers, func(ctx context.Context, name string) model.ItemResult {
		_ = ctx
		result := model.ItemResult{Name: name, Action: "delete", Path: byName[name], ConnectProfile: profiles[name], Warnings: []string{"Git removal deletes the live connector only when ArgoCD pruning is permitted"}}
		path, ok := byName[name]
		if !ok {
			result.Status = "failed"
			result.Error = "manifest was not found in Git"
			return result
		}
		if err := s.Repository.PlanRemove(path); err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			return result
		}
		if !opts.DryRun {
			collector.Add(repository.Mutation{Type: repository.MutationRemove, Path: path})
		}
		result.Status = "succeeded"
		result.Changed = true
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
