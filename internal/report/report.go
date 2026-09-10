package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/example/connectorctl/internal/model"
)

func New(action, cluster, logicalEnv string, dryRun bool) model.RunReport {
	now := time.Now().UTC()
	return model.RunReport{RunID: now.Format("20060102T150405.000000000Z"), StartedAt: now, Action: action, PhysicalCluster: cluster, LogicalEnv: logicalEnv, DryRun: dryRun}
}

func Finalize(r *model.RunReport, items []model.ItemResult) {
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	r.Items = items
	r.FinishedAt = time.Now().UTC()
	r.Succeeded = 0
	r.Failed = 0
	r.Skipped = 0
	for _, item := range items {
		switch item.Status {
		case "succeeded":
			r.Succeeded++
		case "skipped":
			r.Skipped++
		default:
			r.Failed++
		}
	}
}

func Write(dir string, r model.RunReport) (string, error) {
	if dir == "" {
		return "", nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%s-%s.json", r.RunID, r.Action, r.LogicalEnv))
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
