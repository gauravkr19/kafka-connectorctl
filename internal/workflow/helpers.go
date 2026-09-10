package workflow

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/example/connectorctl/internal/config"
)

func configMappings(s *Service, cluster, logicalEnv string) (config.SecretMappingsFile, error) {
	mappings, err := config.LoadSecretMappings(s.Bundle.ConfigDir, strings.ToLower(cluster), strings.ToLower(logicalEnv))
	if err != nil {
		return config.SecretMappingsFile{}, err
	}
	if err := config.ValidateSecretMappings(mappings, s.Bundle.SecretProfiles); err != nil {
		return config.SecretMappingsFile{}, fmt.Errorf("validate secret mappings: %w", err)
	}
	return mappings, nil
}

func matchesEnv(name, pattern string) bool {
	re, err := regexp.Compile(pattern)
	return err == nil && re.MatchString(name)
}

func (s *Service) validateConcurrency(requested int) (int, error) {
	if requested == 0 {
		requested = s.Bundle.Policies.Defaults.Concurrency
	}
	if requested < 1 {
		return 0, fmt.Errorf("concurrency must be greater than zero")
	}
	if requested > s.Bundle.Policies.Defaults.MaxConcurrency {
		return 0, fmt.Errorf("concurrency %d exceeds policy maximum %d", requested, s.Bundle.Policies.Defaults.MaxConcurrency)
	}
	return requested, nil
}
