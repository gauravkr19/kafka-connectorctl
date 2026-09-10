package workflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/example/connectorctl/internal/model"
	"github.com/example/connectorctl/internal/report"
)

type VerifyOptions struct {
	Cluster     string
	LogicalEnv  string
	Selection   SelectionOptions
	Concurrency int
}

func (s *Service) Verify(ctx context.Context, opts VerifyOptions) (model.RunReport, string, error) {
	run := report.New("verify", opts.Cluster, opts.LogicalEnv, true)
	cluster, env, err := s.Bundle.ResolveTarget(opts.Cluster, opts.LogicalEnv)
	if err != nil {
		return run, "", err
	}
	manifests, err := s.Repository.LoadConnectors(cluster.GitRoot + "/" + strings.ToLower(opts.LogicalEnv))
	if err != nil {
		return run, "", err
	}
	profiles := map[string]string{}
	var managedNames []string
	for _, manifest := range manifests {
		name := manifest.Connector.Spec.Name
		if name == "" {
			name = manifest.Connector.Metadata.Name
		}
		if _, duplicate := profiles[name]; duplicate {
			return run, "", fmt.Errorf("Git contains duplicate connector name %q", name)
		}
		profile, err := profileFromCR(manifest.Connector, cluster)
		if err != nil {
			return run, "", err
		}
		profiles[name] = profile
		managedNames = append(managedNames, name)
	}
	names, err := selectNames(managedNames, env.NameRegex, opts.Selection, true, s.Bundle.Policies.Defaults.BatchSize, s.Bundle.Policies.Adoption.MaxBatchSize)
	if err != nil {
		return run, "", err
	}
	workers, err := s.validateConcurrency(opts.Concurrency)
	if err != nil {
		return run, "", err
	}
	clientFor := s.clientProvider(strings.ToLower(opts.Cluster))
	items := runConcurrent(ctx, names, workers, func(ctx context.Context, name string) model.ItemResult {
		result := model.ItemResult{Name: name, Action: "verify", ConnectProfile: profiles[name]}
		client, err := clientFor(profiles[name])
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			return result
		}
		status, err := client.GetStatus(ctx, name)
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			return result
		}
		result.TaskStates = statusStates(status)
		if !strings.EqualFold(status.Connector.State, "RUNNING") {
			result.Status = "failed"
			result.Error = "connector state is " + status.Connector.State
			return result
		}
		for _, task := range status.Tasks {
			if !strings.EqualFold(task.State, "RUNNING") {
				result.Status = "failed"
				result.Error = "one or more tasks are not RUNNING"
				return result
			}
		}
		result.Status = "succeeded"
		return result
	})
	report.Finalize(&run, items)
	reportPath, writeErr := report.Write(s.ReportsDir, run)
	if writeErr != nil {
		return run, "", writeErr
	}
	if run.Failed > 0 {
		return run, reportPath, BatchError{Failed: run.Failed, Total: len(run.Items)}
	}
	return run, reportPath, nil
}
