package config

import "github.com/example/connectorctl/internal/model"

const RegistryAPIVersion = "connectorctl.io/v1alpha1"

type Bundle struct {
	Platforms      PlatformsFile
	Plugins        PluginsFile
	Policies       PoliciesFile
	SecretProfiles SecretProfilesFile
	ConfigDir      string
}

type PlatformsFile struct {
	APIVersion string             `yaml:"apiVersion"`
	Clusters   map[string]Cluster `yaml:"clusters"`
}

type Cluster struct {
	Enabled             bool                      `yaml:"enabled"`
	Namespace           string                    `yaml:"namespace"`
	GitRoot             string                    `yaml:"gitRoot"`
	ArgoApplication     string                    `yaml:"argoApplication,omitempty"`
	LogicalEnvironments map[string]Environment    `yaml:"logicalEnvironments"`
	ConnectProfiles     map[string]ConnectProfile `yaml:"connectProfiles"`
}

type Environment struct {
	NameRegex             string `yaml:"nameRegex"`
	DefaultConnectProfile string `yaml:"defaultConnectProfile"`
	Enabled               *bool  `yaml:"enabled,omitempty"`
}

type RESTConfig struct {
	BaseURL string   `yaml:"baseURL"`
	Auth    RESTAuth `yaml:"auth"`
	TLS     RESTTLS  `yaml:"tls,omitempty"`
	Timeout string   `yaml:"timeout,omitempty"`
	RPS     float64  `yaml:"rps,omitempty"`
	Burst   int      `yaml:"burst,omitempty"`
	Retries int      `yaml:"retries,omitempty"`
}

type RESTAuth struct {
	Type        string `yaml:"type"`
	UsernameEnv string `yaml:"usernameEnv,omitempty"`
	PasswordEnv string `yaml:"passwordEnv,omitempty"`
	TokenEnv    string `yaml:"tokenEnv,omitempty"`
}

type RESTTLS struct {
	InsecureSkipVerify bool   `yaml:"insecureSkipVerify,omitempty"`
	CAFile             string `yaml:"caFile,omitempty"`
	CertFile           string `yaml:"certFile,omitempty"`
	KeyFile            string `yaml:"keyFile,omitempty"`
}

type ConnectProfile struct {
	AdoptionSource    bool                     `yaml:"adoptionSource,omitempty"`
	REST              RESTConfig               `yaml:"rest"`
	ConnectClusterRef *model.NamespacedNameRef `yaml:"connectClusterRef,omitempty"`
	ConnectRest       map[string]any           `yaml:"connectRest,omitempty"`
	RestartPolicy     *model.RestartPolicy     `yaml:"restartPolicy,omitempty"`
}

type PluginsFile struct {
	APIVersion string                `yaml:"apiVersion"`
	Plugins    map[string]PluginRule `yaml:"plugins"`
}

type PluginRule struct {
	Classes               []string `yaml:"classes"`
	Type                  string   `yaml:"type,omitempty"`
	DefaultConnectProfile string   `yaml:"defaultConnectProfile,omitempty"`
	Required              []string `yaml:"required,omitempty"`
	Sensitive             []string `yaml:"sensitive,omitempty"`
	SensitivePatterns     []string `yaml:"sensitivePatterns,omitempty"`
}

type PoliciesFile struct {
	APIVersion string            `yaml:"apiVersion"`
	Defaults   DefaultPolicies   `yaml:"defaults"`
	Adoption   AdoptionPolicy    `yaml:"adoption"`
	Create     MutationPolicy    `yaml:"create"`
	Update     MutationPolicy    `yaml:"update"`
	Delete     DeletePolicy      `yaml:"delete"`
	Validation ValidationPolicy  `yaml:"validation"`
	Operations OperationsPolicy  `yaml:"operations"`
	Approvals  map[string]string `yaml:"approvals,omitempty"`
}

type DefaultPolicies struct {
	Concurrency    int `yaml:"concurrency"`
	MaxConcurrency int `yaml:"maxConcurrency"`
	BatchSize      int `yaml:"batchSize"`
}

type AdoptionPolicy struct {
	MaxBatchSize          int  `yaml:"maxBatchSize"`
	AllowWholeEnvironment bool `yaml:"allowWholeEnvironment"`
	RequireFunctionalDiff bool `yaml:"requireFunctionalDiff"`
}

type MutationPolicy struct {
	RequireDryRun       bool `yaml:"requireDryRun"`
	RequireChangeTicket bool `yaml:"requireChangeTicket,omitempty"`
}

type DeletePolicy struct {
	RequireManualApproval bool `yaml:"requireManualApproval"`
	RequireChangeTicket   bool `yaml:"requireChangeTicket"`
}

type ValidationPolicy struct {
	VerifyPluginInstalled    bool     `yaml:"verifyPluginInstalled"`
	ValidateAgainstConnect   bool     `yaml:"validateAgainstConnectAPI"`
	ServerDryRun             bool     `yaml:"serverDryRun"`
	RejectPlaintextSecrets   bool     `yaml:"rejectPlaintextSecrets"`
	GenericSensitivePatterns []string `yaml:"genericSensitivePatterns,omitempty"`
}

type OperationsPolicy struct {
	Allowed []string `yaml:"allowed"`
}

type SecretProfilesFile struct {
	APIVersion string                   `yaml:"apiVersion"`
	Profiles   map[string]SecretProfile `yaml:"profiles"`
	Security   SecretSecurity           `yaml:"security"`
}

type SecretProfile struct {
	Type        string `yaml:"type"`
	MountRoot   string `yaml:"mountRoot,omitempty"`
	DefaultFile string `yaml:"defaultFile,omitempty"`
}

type SecretSecurity struct {
	NeverPersistPlaintext bool `yaml:"neverPersistPlaintext"`
	NeverLogSecretValues  bool `yaml:"neverLogSecretValues"`
}

type SecretMappingsFile struct {
	APIVersion string                      `yaml:"apiVersion"`
	Connectors map[string]ConnectorMapping `yaml:"connectors"`
}

type ConnectorMapping struct {
	ConnectProfile string                    `yaml:"connectProfile,omitempty"`
	Fields         map[string]SecretFieldRef `yaml:"fields,omitempty"`
}

type SecretFieldRef struct {
	Profile          string `yaml:"profile,omitempty"`
	SecretName       string `yaml:"secretName,omitempty"`
	SecretKey        string `yaml:"secretKey,omitempty"`
	FileName         string `yaml:"fileName,omitempty"`
	MountPath        string `yaml:"mountPath,omitempty"`
	LiteralReference string `yaml:"literalReference,omitempty"`
}
