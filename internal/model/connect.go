package model

type PluginInfo struct {
	Class   string `json:"class"`
	Type    string `json:"type"`
	Version string `json:"version"`
}

type ConnectorStatus struct {
	Name      string          `json:"name"`
	Connector StatusComponent `json:"connector"`
	Tasks     []TaskStatus    `json:"tasks"`
	Type      string          `json:"type,omitempty"`
}

type StatusComponent struct {
	State    string `json:"state"`
	WorkerID string `json:"worker_id"`
	Trace    string `json:"trace,omitempty"`
}

type TaskStatus struct {
	ID       int    `json:"id"`
	State    string `json:"state"`
	WorkerID string `json:"worker_id"`
	Trace    string `json:"trace,omitempty"`
}

type ConfigValidationResponse struct {
	Name       string                 `json:"name"`
	ErrorCount int                    `json:"error_count"`
	Groups     []string               `json:"groups,omitempty"`
	Configs    []ConfigValidationItem `json:"configs,omitempty"`
}

type ConfigValidationItem struct {
	Definition map[string]any `json:"definition,omitempty"`
	Value      map[string]any `json:"value,omitempty"`
}
