package config

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var safeKey = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func (b *Bundle) Validate() error {
	var problems []string
	for file, version := range map[string]string{
		"platforms.yaml":       b.Platforms.APIVersion,
		"plugins.yaml":         b.Plugins.APIVersion,
		"policies.yaml":        b.Policies.APIVersion,
		"secret-profiles.yaml": b.SecretProfiles.APIVersion,
	} {
		if version != RegistryAPIVersion {
			problems = append(problems, fmt.Sprintf("%s: apiVersion must be %q", file, RegistryAPIVersion))
		}
	}
	if len(b.Platforms.Clusters) == 0 {
		problems = append(problems, "platforms.yaml: no clusters configured")
	}
	environmentOwners := map[string]string{}
	gitRootOwners := map[string]string{}
	for clusterName, cluster := range b.Platforms.Clusters {
		prefix := fmt.Sprintf("platforms.yaml cluster %q", clusterName)
		if clusterName != strings.ToLower(clusterName) || !safeKey.MatchString(clusterName) {
			problems = append(problems, prefix+": key must be lowercase DNS-style text")
		}
		if strings.TrimSpace(cluster.Namespace) == "" {
			problems = append(problems, prefix+": namespace is required")
		}
		if strings.TrimSpace(cluster.GitRoot) == "" {
			problems = append(problems, prefix+": gitRoot is required")
		} else if filepath.IsAbs(cluster.GitRoot) || containsParentTraversal(cluster.GitRoot) {
			problems = append(problems, prefix+": gitRoot must be a relative path without '..'")
		} else {
			cleanRoot := filepath.Clean(cluster.GitRoot)
			if owner, exists := gitRootOwners[cleanRoot]; exists && owner != clusterName {
				problems = append(problems, fmt.Sprintf("%s: gitRoot is already used by cluster %q", prefix, owner))
			} else {
				gitRootOwners[cleanRoot] = clusterName
			}
		}
		if len(cluster.LogicalEnvironments) == 0 {
			problems = append(problems, prefix+": at least one logical environment is required")
		}
		if len(cluster.ConnectProfiles) == 0 {
			problems = append(problems, prefix+": at least one connect profile is required")
		}
		for envName, env := range cluster.LogicalEnvironments {
			envPrefix := fmt.Sprintf("%s environment %q", prefix, envName)
			if envName != strings.ToLower(envName) || !safeKey.MatchString(envName) {
				problems = append(problems, envPrefix+": key must be lowercase DNS-style text")
			}
			if owner, exists := environmentOwners[envName]; exists && owner != clusterName {
				problems = append(problems, fmt.Sprintf("%s: logical environment is already mapped to cluster %q", envPrefix, owner))
			} else {
				environmentOwners[envName] = clusterName
			}
			if env.NameRegex == "" {
				problems = append(problems, envPrefix+": nameRegex is required")
			} else {
				if !strings.HasPrefix(env.NameRegex, "^") {
					problems = append(problems, envPrefix+": nameRegex must be anchored with '^'")
				}
				if _, err := regexp.Compile(env.NameRegex); err != nil {
					problems = append(problems, fmt.Sprintf("%s: invalid nameRegex: %v", envPrefix, err))
				}
			}
			profile := env.DefaultConnectProfile
			if profile == "" {
				profile = "default"
			}
			if _, ok := cluster.ConnectProfiles[profile]; !ok {
				problems = append(problems, fmt.Sprintf("%s: connect profile %q does not exist", envPrefix, profile))
			}
		}
		for profileName, profile := range cluster.ConnectProfiles {
			profilePrefix := fmt.Sprintf("%s connect profile %q", prefix, profileName)
			if profileName != strings.ToLower(profileName) || !safeKey.MatchString(profileName) {
				problems = append(problems, profilePrefix+": key must be lowercase DNS-style text")
			}
			if profile.ConnectClusterRef == nil && len(profile.ConnectRest) == 0 {
				problems = append(problems, profilePrefix+": connectClusterRef or connectRest is required")
			}
			if profile.ConnectClusterRef != nil && strings.TrimSpace(profile.ConnectClusterRef.Name) == "" {
				problems = append(problems, profilePrefix+": connectClusterRef.name is required")
			}
			if profile.RestartPolicy != nil {
				switch strings.ToLower(profile.RestartPolicy.Type) {
				case "onfailure", "never":
				default:
					problems = append(problems, profilePrefix+": restartPolicy.type must be OnFailure or Never")
				}
				if profile.RestartPolicy.MaxRetry < 0 {
					problems = append(problems, profilePrefix+": restartPolicy.maxRetry cannot be negative")
				}
			}
			if cluster.Enabled {
				problems = append(problems, validateREST(profilePrefix+" rest", profile.REST)...)
			}
		}
	}

	classOwners := map[string]string{}
	for alias, plugin := range b.Plugins.Plugins {
		prefix := fmt.Sprintf("plugins.yaml plugin %q", alias)
		if alias != strings.ToLower(alias) || !safeKey.MatchString(alias) {
			problems = append(problems, prefix+": alias must be lowercase DNS-style text")
		}
		if len(plugin.Classes) == 0 {
			problems = append(problems, prefix+": at least one class is required")
		}
		for _, className := range plugin.Classes {
			className = strings.TrimSpace(className)
			if className == "" {
				problems = append(problems, prefix+": class cannot be empty")
				continue
			}
			if owner, exists := classOwners[className]; exists {
				problems = append(problems, fmt.Sprintf("%s: class %q already belongs to plugin %q", prefix, className, owner))
			} else {
				classOwners[className] = alias
			}
		}
		for _, pattern := range plugin.SensitivePatterns {
			if _, err := regexp.Compile(pattern); err != nil {
				problems = append(problems, fmt.Sprintf("%s: invalid sensitive pattern %q: %v", prefix, pattern, err))
			}
		}
		if plugin.DefaultConnectProfile != "" && !safeKey.MatchString(plugin.DefaultConnectProfile) {
			problems = append(problems, prefix+": defaultConnectProfile must be a lowercase profile key")
		}
	}
	for _, pattern := range b.Policies.Validation.GenericSensitivePatterns {
		if _, err := regexp.Compile(pattern); err != nil {
			problems = append(problems, fmt.Sprintf("policies.yaml: invalid generic sensitive pattern %q: %v", pattern, err))
		}
	}
	if b.Policies.Defaults.Concurrency <= 0 {
		problems = append(problems, "policies.yaml: defaults.concurrency must be greater than zero")
	}
	if b.Policies.Defaults.MaxConcurrency < b.Policies.Defaults.Concurrency {
		problems = append(problems, "policies.yaml: defaults.maxConcurrency must be >= defaults.concurrency")
	}
	if b.Policies.Defaults.BatchSize <= 0 {
		problems = append(problems, "policies.yaml: defaults.batchSize must be greater than zero")
	}
	if b.Policies.Adoption.MaxBatchSize <= 0 {
		problems = append(problems, "policies.yaml: adoption.maxBatchSize must be greater than zero")
	} else if b.Policies.Defaults.BatchSize > b.Policies.Adoption.MaxBatchSize {
		problems = append(problems, "policies.yaml: defaults.batchSize must be <= adoption.maxBatchSize")
	}
	knownOperations := map[string]struct{}{
		"status": {}, "pause": {}, "resume": {}, "restart": {}, "restart-task": {},
	}
	seenOperations := map[string]struct{}{}
	for _, operation := range b.Policies.Operations.Allowed {
		op := strings.ToLower(strings.TrimSpace(operation))
		if _, ok := knownOperations[op]; !ok {
			problems = append(problems, fmt.Sprintf("policies.yaml: unsupported operation %q", operation))
		}
		if _, duplicate := seenOperations[op]; duplicate {
			problems = append(problems, fmt.Sprintf("policies.yaml: duplicate operation %q", operation))
		}
		seenOperations[op] = struct{}{}
	}
	if !b.Policies.Validation.RejectPlaintextSecrets {
		problems = append(problems, "policies.yaml: validation.rejectPlaintextSecrets must be true")
	}
	if !b.SecretProfiles.Security.NeverPersistPlaintext {
		problems = append(problems, "secret-profiles.yaml: security.neverPersistPlaintext must be true")
	}
	if !b.SecretProfiles.Security.NeverLogSecretValues {
		problems = append(problems, "secret-profiles.yaml: security.neverLogSecretValues must be true")
	}
	for name, profile := range b.SecretProfiles.Profiles {
		prefix := fmt.Sprintf("secret-profiles.yaml profile %q", name)
		if strings.TrimSpace(profile.Type) == "" {
			problems = append(problems, prefix+": type is required")
		}
		if profile.MountRoot != "" && (!strings.HasPrefix(profile.MountRoot, "/") || strings.ContainsAny(profile.MountRoot, ":\r\n") || filepath.Clean(profile.MountRoot) != profile.MountRoot) {
			problems = append(problems, prefix+": mountRoot must be an absolute, clean path without ':' or newlines")
		}
		if profile.DefaultFile != "" && (filepath.Base(profile.DefaultFile) != profile.DefaultFile || profile.DefaultFile == "." || profile.DefaultFile == "..") {
			problems = append(problems, prefix+": defaultFile must be a single file name")
		}
	}
	if _, ok := b.SecretProfiles.Profiles["mounted-file"]; !ok {
		problems = append(problems, "secret-profiles.yaml: profile \"mounted-file\" is required")
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return errors.New(strings.Join(problems, "\n"))
	}
	return nil
}

func validateREST(prefix string, cfg RESTConfig) []string {
	var problems []string
	if strings.TrimSpace(cfg.BaseURL) == "" {
		problems = append(problems, prefix+": baseURL is required")
	} else if u, err := url.Parse(cfg.BaseURL); err != nil || u.Scheme == "" || u.Host == "" {
		problems = append(problems, prefix+": baseURL must be an absolute URL")
	}
	if cfg.Timeout != "" {
		if _, err := time.ParseDuration(cfg.Timeout); err != nil {
			problems = append(problems, prefix+": timeout is invalid: "+err.Error())
		}
	}
	if cfg.RPS < 0 {
		problems = append(problems, prefix+": rps cannot be negative")
	}
	if cfg.Burst < 0 {
		problems = append(problems, prefix+": burst cannot be negative")
	}
	if cfg.Retries < 0 {
		problems = append(problems, prefix+": retries cannot be negative")
	}
	switch strings.ToLower(cfg.Auth.Type) {
	case "", "none":
	case "basic":
		if cfg.Auth.UsernameEnv == "" || cfg.Auth.PasswordEnv == "" {
			problems = append(problems, prefix+": basic auth requires usernameEnv and passwordEnv")
		}
	case "bearer":
		if cfg.Auth.TokenEnv == "" {
			problems = append(problems, prefix+": bearer auth requires tokenEnv")
		}
	default:
		problems = append(problems, prefix+": unsupported auth.type "+cfg.Auth.Type)
	}
	if (cfg.TLS.CertFile == "") != (cfg.TLS.KeyFile == "") {
		problems = append(problems, prefix+": tls.certFile and tls.keyFile must be supplied together")
	}
	return problems
}

func containsParentTraversal(value string) bool {
	for _, element := range strings.FieldsFunc(filepath.ToSlash(value), func(r rune) bool { return r == '/' }) {
		if element == ".." {
			return true
		}
	}
	return false
}

func (b *Bundle) ResolveTarget(clusterName, logicalEnv string) (Cluster, Environment, error) {
	clusterKey := strings.ToLower(strings.TrimSpace(clusterName))
	envKey := strings.ToLower(strings.TrimSpace(logicalEnv))
	cluster, ok := b.Platforms.Clusters[clusterKey]
	if !ok {
		return Cluster{}, Environment{}, fmt.Errorf("unknown physical cluster %q", clusterName)
	}
	if !cluster.Enabled {
		return Cluster{}, Environment{}, fmt.Errorf("physical cluster %q is disabled", clusterName)
	}
	env, ok := cluster.LogicalEnvironments[envKey]
	if !ok {
		for otherCluster, candidate := range b.Platforms.Clusters {
			if _, exists := candidate.LogicalEnvironments[envKey]; exists {
				return Cluster{}, Environment{}, fmt.Errorf("invalid target: logical environment %s is mapped to physical cluster %s; requested cluster %s", strings.ToUpper(envKey), strings.ToUpper(otherCluster), strings.ToUpper(clusterKey))
			}
		}
		return Cluster{}, Environment{}, fmt.Errorf("logical environment %q is not configured", logicalEnv)
	}
	if env.Enabled != nil && !*env.Enabled {
		return Cluster{}, Environment{}, fmt.Errorf("logical environment %q is disabled", logicalEnv)
	}
	return cluster, env, nil
}
