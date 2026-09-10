package model

type CFKConnector struct {
	APIVersion string        `yaml:"apiVersion" json:"apiVersion"`
	Kind       string        `yaml:"kind" json:"kind"`
	Metadata   ObjectMeta    `yaml:"metadata" json:"metadata"`
	Spec       ConnectorSpec `yaml:"spec" json:"spec"`
}

type ObjectMeta struct {
	Name        string            `yaml:"name" json:"name"`
	Namespace   string            `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty" json:"annotations,omitempty"`
}

type ConnectorSpec struct {
	Name              string             `yaml:"name,omitempty" json:"name,omitempty"`
	Class             string             `yaml:"class" json:"class"`
	TaskMax           int                `yaml:"taskMax" json:"taskMax"`
	ConnectClusterRef *NamespacedNameRef `yaml:"connectClusterRef,omitempty" json:"connectClusterRef,omitempty"`
	ConnectRest       map[string]any     `yaml:"connectRest,omitempty" json:"connectRest,omitempty"`
	Configs           map[string]string  `yaml:"configs" json:"configs"`
	RestartPolicy     RestartPolicy      `yaml:"restartPolicy" json:"restartPolicy"`
}

type NamespacedNameRef struct {
	Name      string `yaml:"name" json:"name"`
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`
}

type RestartPolicy struct {
	Type     string `yaml:"type" json:"type"`
	MaxRetry int    `yaml:"maxRetry,omitempty" json:"maxRetry,omitempty"`
}

type NormalizedConnector struct {
	Name            string
	PhysicalCluster string
	LogicalEnv      string
	PluginAlias     string
	Class           string
	TaskMax         int
	ConnectProfile  string
	Origin          string
	ChangeTicket    string
	Configs         map[string]string
}
