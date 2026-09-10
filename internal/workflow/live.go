package workflow

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/example/connectorctl/internal/config"
	"github.com/example/connectorctl/internal/connect"
	"github.com/example/connectorctl/internal/model"
)

type profileNamesResult struct {
	profile string
	names   []string
	err     error
}

type profilePluginsResult struct {
	profile   string
	installed map[string]struct{}
	err       error
}

func discoveryProfiles(cluster config.Cluster, env config.Environment, explicit string) ([]string, error) {
	if explicit = strings.ToLower(strings.TrimSpace(explicit)); explicit != "" {
		if _, ok := cluster.ConnectProfiles[explicit]; !ok {
			return nil, fmt.Errorf("connect profile %q is not configured", explicit)
		}
		return []string{explicit}, nil
	}
	var profiles []string
	for name, profile := range cluster.ConnectProfiles {
		if profile.AdoptionSource {
			profiles = append(profiles, name)
		}
	}
	if len(profiles) == 0 {
		name := strings.ToLower(strings.TrimSpace(env.DefaultConnectProfile))
		if name == "" {
			name = "default"
		}
		if _, ok := cluster.ConnectProfiles[name]; !ok {
			return nil, fmt.Errorf("default connect profile %q is not configured", name)
		}
		profiles = append(profiles, name)
	}
	sort.Strings(profiles)
	return profiles, nil
}

func allProfileNames(cluster config.Cluster) []string {
	profiles := make([]string, 0, len(cluster.ConnectProfiles))
	for name := range cluster.ConnectProfiles {
		profiles = append(profiles, name)
	}
	sort.Strings(profiles)
	return profiles
}

func (s *Service) discoverLiveConnectors(ctx context.Context, clusterName string, cluster config.Cluster, env config.Environment, explicitProfile string) (map[string]string, error) {
	profiles, err := discoveryProfiles(cluster, env, explicitProfile)
	if err != nil {
		return nil, err
	}
	return s.discoverLiveConnectorsInProfiles(ctx, clusterName, env, profiles)
}

func (s *Service) discoverAllLiveConnectors(ctx context.Context, clusterName string, cluster config.Cluster, env config.Environment) (map[string]string, error) {
	return s.discoverLiveConnectorsInProfiles(ctx, clusterName, env, allProfileNames(cluster))
}

func (s *Service) discoverLiveConnectorsInProfiles(ctx context.Context, clusterName string, env config.Environment, profiles []string) (map[string]string, error) {
	byProfile, err := s.loadConnectorNamesByProfile(ctx, clusterName, profiles)
	if err != nil {
		return nil, err
	}
	re, err := regexp.Compile(env.NameRegex)
	if err != nil {
		return nil, err
	}
	locations := map[string]string{}
	for profile, names := range byProfile {
		for _, name := range names {
			if !re.MatchString(name) {
				continue
			}
			if previous, exists := locations[name]; exists && previous != profile {
				return nil, fmt.Errorf("connector %q exists in multiple Connect profiles (%s and %s)", name, previous, profile)
			}
			locations[name] = profile
		}
	}
	return locations, nil
}

func (s *Service) loadConnectorNamesByProfile(ctx context.Context, clusterName string, profiles []string) (map[string][]string, error) {
	results := make(chan profileNamesResult, len(profiles))
	var wg sync.WaitGroup
	for _, profile := range uniqueSorted(profiles) {
		profile := profile
		wg.Add(1)
		go func() {
			defer wg.Done()
			client, err := s.ClientFor(clusterName, profile)
			if err != nil {
				results <- profileNamesResult{profile: profile, err: err}
				return
			}
			names, err := client.ListConnectors(ctx)
			results <- profileNamesResult{profile: profile, names: names, err: err}
		}()
	}
	wg.Wait()
	close(results)
	out := map[string][]string{}
	for result := range results {
		if result.err != nil {
			return nil, fmt.Errorf("list connectors from profile %s: %w", result.profile, result.err)
		}
		out[result.profile] = result.names
	}
	return out, nil
}

func flattenProfileNames(byProfile map[string][]string) (map[string]string, error) {
	locations := map[string]string{}
	for profile, names := range byProfile {
		for _, name := range names {
			if previous, exists := locations[name]; exists && previous != profile {
				return nil, fmt.Errorf("connector %q exists in multiple Connect profiles (%s and %s)", name, previous, profile)
			}
			locations[name] = profile
		}
	}
	return locations, nil
}

func (s *Service) loadInstalledByProfile(ctx context.Context, clusterName string, profiles []string) (map[string]map[string]struct{}, error) {
	results := make(chan profilePluginsResult, len(profiles))
	var wg sync.WaitGroup
	for _, profile := range uniqueSorted(profiles) {
		profile := profile
		wg.Add(1)
		go func() {
			defer wg.Done()
			client, err := s.ClientFor(clusterName, profile)
			if err != nil {
				results <- profilePluginsResult{profile: profile, err: err}
				return
			}
			plugins, err := client.ListPlugins(ctx)
			if err != nil {
				results <- profilePluginsResult{profile: profile, err: err}
				return
			}
			results <- profilePluginsResult{profile: profile, installed: installedSet(plugins)}
		}()
	}
	wg.Wait()
	close(results)
	out := map[string]map[string]struct{}{}
	for result := range results {
		if result.err != nil {
			return nil, fmt.Errorf("list plugins from profile %s: %w", result.profile, result.err)
		}
		out[result.profile] = result.installed
	}
	return out, nil
}

func (s *Service) clientProvider(clusterName string) clientProvider {
	return func(profile string) (*connect.Client, error) {
		return s.ClientFor(clusterName, profile)
	}
}

func profileFromCR(cr model.CFKConnector, cluster config.Cluster) (string, error) {
	if label := strings.TrimSpace(cr.Metadata.Labels["connectorctl.io/connect-profile"]); label != "" {
		profile, ok := cluster.ConnectProfiles[label]
		if !ok {
			return "", fmt.Errorf("CR %s references unknown connect profile %q", cr.Metadata.Name, label)
		}
		if !equalRef(cr.Spec.ConnectClusterRef, profile.ConnectClusterRef) || !reflect.DeepEqual(cr.Spec.ConnectRest, profile.ConnectRest) {
			return "", fmt.Errorf("CR %s connect-profile label %q does not match its discovery fields", cr.Metadata.Name, label)
		}
		return label, nil
	}
	var matches []string
	for name, profile := range cluster.ConnectProfiles {
		if equalRef(cr.Spec.ConnectClusterRef, profile.ConnectClusterRef) && reflect.DeepEqual(cr.Spec.ConnectRest, profile.ConnectRest) {
			matches = append(matches, name)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("cannot map CR %s discovery fields to a configured connect profile", cr.Metadata.Name)
	}
	sort.Strings(matches)
	return "", fmt.Errorf("CR %s discovery fields match multiple connect profiles: %s", cr.Metadata.Name, strings.Join(matches, ", "))
}

func equalRef(a, b *model.NamespacedNameRef) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Name == b.Name && a.Namespace == b.Namespace
}

func uniqueSorted(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			set[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
