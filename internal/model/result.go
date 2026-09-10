package model

import "time"

type ItemResult struct {
	Name           string   `json:"name" yaml:"name"`
	Action         string   `json:"action" yaml:"action"`
	Status         string   `json:"status" yaml:"status"`
	Plugin         string   `json:"plugin,omitempty" yaml:"plugin,omitempty"`
	ConnectProfile string   `json:"connectProfile,omitempty" yaml:"connectProfile,omitempty"`
	Path           string   `json:"path,omitempty" yaml:"path,omitempty"`
	PreviewPath    string   `json:"previewPath,omitempty" yaml:"previewPath,omitempty"`
	Changed        bool     `json:"changed,omitempty" yaml:"changed,omitempty"`
	Warnings       []string `json:"warnings,omitempty" yaml:"warnings,omitempty"`
	Error          string   `json:"error,omitempty" yaml:"error,omitempty"`
	TaskStates     []string `json:"taskStates,omitempty" yaml:"taskStates,omitempty"`
}

type RunReport struct {
	RunID           string       `json:"runId" yaml:"runId"`
	StartedAt       time.Time    `json:"startedAt" yaml:"startedAt"`
	FinishedAt      time.Time    `json:"finishedAt" yaml:"finishedAt"`
	Action          string       `json:"action" yaml:"action"`
	PhysicalCluster string       `json:"physicalCluster" yaml:"physicalCluster"`
	LogicalEnv      string       `json:"logicalEnv" yaml:"logicalEnv"`
	DryRun          bool         `json:"dryRun" yaml:"dryRun"`
	ChangeTicket    string       `json:"changeTicket,omitempty" yaml:"changeTicket,omitempty"`
	Succeeded       int          `json:"succeeded" yaml:"succeeded"`
	Failed          int          `json:"failed" yaml:"failed"`
	Skipped         int          `json:"skipped" yaml:"skipped"`
	Items           []ItemResult `json:"items" yaml:"items"`
}
