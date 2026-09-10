package workflow

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type ProfileMismatch struct {
	Name        string `json:"name" yaml:"name"`
	LiveProfile string `json:"liveProfile" yaml:"liveProfile"`
	GitProfile  string `json:"gitProfile" yaml:"gitProfile"`
}

type InventoryResult struct {
	PhysicalCluster string            `json:"physicalCluster" yaml:"physicalCluster"`
	LogicalEnv      string            `json:"logicalEnv" yaml:"logicalEnv"`
	LiveCount       int               `json:"liveCount" yaml:"liveCount"`
	GitCount        int               `json:"gitCount" yaml:"gitCount"`
	LiveByProfile   map[string]int    `json:"liveByProfile" yaml:"liveByProfile"`
	GitByProfile    map[string]int    `json:"gitByProfile" yaml:"gitByProfile"`
	Adopted         []string          `json:"adopted" yaml:"adopted"`
	Pending         []string          `json:"pending" yaml:"pending"`
	GitOnly         []string          `json:"gitOnly" yaml:"gitOnly"`
	ProfileMismatch []ProfileMismatch `json:"profileMismatch,omitempty" yaml:"profileMismatch,omitempty"`
}

func (s *Service) Inventory(ctx context.Context, clusterName, logicalEnv string) (InventoryResult, error) {
	cluster, env, err := s.Bundle.ResolveTarget(clusterName, logicalEnv)
	if err != nil {
		return InventoryResult{}, err
	}
	liveLocations, err := s.discoverAllLiveConnectors(ctx, strings.ToLower(clusterName), cluster, env)
	if err != nil {
		return InventoryResult{}, err
	}
	manifests, err := s.Repository.LoadConnectors(cluster.GitRoot + "/" + strings.ToLower(logicalEnv))
	if err != nil {
		return InventoryResult{}, err
	}
	gitProfiles := map[string]string{}
	for _, manifest := range manifests {
		name := manifest.Connector.Spec.Name
		if name == "" {
			name = manifest.Connector.Metadata.Name
		}
		if _, exists := gitProfiles[name]; exists {
			return InventoryResult{}, fmt.Errorf("Git contains duplicate connector name %q under environment %s", name, logicalEnv)
		}
		profile, err := profileFromCR(manifest.Connector, cluster)
		if err != nil {
			return InventoryResult{}, err
		}
		gitProfiles[name] = profile
	}
	result := InventoryResult{
		PhysicalCluster: strings.ToLower(clusterName), LogicalEnv: strings.ToLower(logicalEnv),
		LiveCount: len(liveLocations), GitCount: len(gitProfiles), LiveByProfile: map[string]int{}, GitByProfile: map[string]int{},
	}
	for name, profile := range liveLocations {
		result.LiveByProfile[profile]++
		if gitProfile, ok := gitProfiles[name]; ok {
			if gitProfile == profile {
				result.Adopted = append(result.Adopted, name)
			} else {
				result.ProfileMismatch = append(result.ProfileMismatch, ProfileMismatch{Name: name, LiveProfile: profile, GitProfile: gitProfile})
			}
		} else {
			result.Pending = append(result.Pending, name)
		}
	}
	for name, profile := range gitProfiles {
		result.GitByProfile[profile]++
		if _, ok := liveLocations[name]; !ok {
			result.GitOnly = append(result.GitOnly, name)
		}
	}
	sort.Strings(result.Adopted)
	sort.Strings(result.Pending)
	sort.Strings(result.GitOnly)
	sort.Slice(result.ProfileMismatch, func(i, j int) bool { return result.ProfileMismatch[i].Name < result.ProfileMismatch[j].Name })
	return result, nil
}
