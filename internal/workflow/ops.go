package workflow

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/example/connectorctl/internal/model"
	"github.com/example/connectorctl/internal/report"
)

type OpsOptions struct {
	Cluster     string
	LogicalEnv  string
	Operation   string
	Selection   SelectionOptions
	Concurrency int
	TaskID      int
	DryRun      bool
}

func (s *Service) Operate(ctx context.Context, opts OpsOptions) (model.RunReport, string, error) {
	op := strings.ToLower(strings.TrimSpace(opts.Operation))
	run := report.New(op, opts.Cluster, opts.LogicalEnv, opts.DryRun)
	cluster, env, err := s.Bundle.ResolveTarget(opts.Cluster, opts.LogicalEnv)
	if err != nil {
		return run, "", err
	}
	if !containsFold(s.Bundle.Policies.Operations.Allowed, op) {
		return run, "", fmt.Errorf("operation %q is not allowed by policy", op)
	}
	base := cluster.GitRoot + "/" + strings.ToLower(opts.LogicalEnv)
	manifests, err := s.Repository.LoadConnectors(base)
	if err != nil {
		return run, "", err
	}
	crNames := map[string]string{}
	profiles := map[string]string{}
	var managedNames []string
	for _, manifest := range manifests {
		name := manifest.Connector.Spec.Name
		if name == "" {
			name = manifest.Connector.Metadata.Name
		}
		if _, duplicate := crNames[name]; duplicate {
			return run, "", fmt.Errorf("Git contains duplicate connector name %q", name)
		}
		profile, err := profileFromCR(manifest.Connector, cluster)
		if err != nil {
			return run, "", err
		}
		crNames[name] = manifest.Connector.Metadata.Name
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
		result := model.ItemResult{Name: name, Action: op, ConnectProfile: profiles[name]}
		if op == "status" {
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
			result.Status = "succeeded"
			return result
		}
		annotation, value, err := operationAnnotation(op, opts.TaskID)
		if err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			return result
		}
		crName, ok := crNames[name]
		if !ok {
			result.Status = "failed"
			result.Error = "managed Connector CR was not found in Git"
			return result
		}
		if !opts.DryRun {
			cmd := exec.CommandContext(ctx, s.KubectlBin, "annotate", "connector", crName, "-n", cluster.Namespace, annotation+"="+value, "--overwrite")
			output, err := cmd.CombinedOutput()
			if err != nil {
				result.Status = "failed"
				result.Error = fmt.Sprintf("annotation command failed: %v: %s", err, strings.TrimSpace(string(output)))
				return result
			}
		}
		result.Status = "succeeded"
		result.Changed = true
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

func operationAnnotation(op string, taskID int) (string, string, error) {
	switch op {
	case "pause":
		return "platform.confluent.io/pause-connector", "true", nil
	case "resume":
		return "platform.confluent.io/resume-connector", "true", nil
	case "restart":
		return "platform.confluent.io/restart-connector", "true", nil
	case "restart-task":
		if taskID < 0 {
			return "", "", fmt.Errorf("task ID must be zero or greater")
		}
		return "platform.confluent.io/restart-task", strconv.Itoa(taskID), nil
	default:
		return "", "", fmt.Errorf("unsupported operation %q", op)
	}
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}
