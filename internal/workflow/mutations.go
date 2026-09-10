package workflow

import (
	"fmt"
	"sync"

	"github.com/example/connectorctl/internal/model"
	"github.com/example/connectorctl/internal/report"
	"github.com/example/connectorctl/internal/repository"
)

type mutationCollector struct {
	mu        sync.Mutex
	mutations []repository.Mutation
}

func (c *mutationCollector) Add(mutations ...repository.Mutation) {
	if len(mutations) == 0 {
		return
	}
	c.mu.Lock()
	c.mutations = append(c.mutations, mutations...)
	c.mu.Unlock()
}

func (c *mutationCollector) Snapshot() []repository.Mutation {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]repository.Mutation(nil), c.mutations...)
}

func applyCollectedMutations(repo *repository.Repository, run *model.RunReport, collector *mutationCollector) error {
	if run.DryRun {
		return nil
	}
	if run.Failed > 0 {
		for i := range run.Items {
			if run.Items[i].Status == "succeeded" && run.Items[i].Changed {
				run.Items[i].Status = "skipped"
				run.Items[i].Changed = false
				run.Items[i].Warnings = append(run.Items[i].Warnings, "worktree change was not applied because another item in the batch failed")
			}
		}
		report.Finalize(run, run.Items)
		return nil
	}
	if err := repo.ApplyBatch(collector.Snapshot()); err != nil {
		for i := range run.Items {
			if run.Items[i].Status == "succeeded" && run.Items[i].Changed {
				run.Items[i].Status = "failed"
				run.Items[i].Error = "worktree batch was not applied: " + err.Error()
			}
		}
		report.Finalize(run, run.Items)
		return fmt.Errorf("apply worktree batch: %w", err)
	}
	return nil
}
