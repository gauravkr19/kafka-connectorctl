package workflow

import (
	"fmt"
	"strings"
	"sync"

	"github.com/example/connectorctl/internal/config"
	"github.com/example/connectorctl/internal/connect"
	"github.com/example/connectorctl/internal/registry"
	"github.com/example/connectorctl/internal/repository"
	"github.com/example/connectorctl/internal/secret"
	"github.com/example/connectorctl/internal/transform"
)

type Service struct {
	Bundle     *config.Bundle
	Registry   *registry.Registry
	Secrets    *secret.Resolver
	Renderer   *transform.Renderer
	Repository *repository.Repository
	ReportsDir string
	RenderDir  string
	KubectlBin string

	clientMu sync.Mutex
	clients  map[string]*connect.Client
}

func NewService(configDir, repoRoot, reportsDir, kubectlBin string, renderDirs ...string) (*Service, error) {
	bundle, err := config.LoadBundle(configDir)
	if err != nil {
		return nil, err
	}
	reg, err := registry.New(bundle.Plugins, bundle.Policies.Validation.GenericSensitivePatterns)
	if err != nil {
		return nil, err
	}
	if kubectlBin == "" {
		kubectlBin = "oc"
	}
	renderDir := "rendered"
	if len(renderDirs) > 0 {
		renderDir = renderDirs[0]
	}
	return &Service{
		Bundle: bundle, Registry: reg, Secrets: secret.NewResolver(bundle.SecretProfiles), Renderer: transform.NewRenderer(),
		Repository: repository.New(repoRoot), ReportsDir: reportsDir, RenderDir: renderDir, KubectlBin: kubectlBin, clients: map[string]*connect.Client{},
	}, nil
}

func (s *Service) ClientFor(clusterName, profileName string) (*connect.Client, error) {
	clusterKey := strings.ToLower(strings.TrimSpace(clusterName))
	profileKey := strings.ToLower(strings.TrimSpace(profileName))
	cluster, ok := s.Bundle.Platforms.Clusters[clusterKey]
	if !ok {
		return nil, fmt.Errorf("unknown physical cluster %q", clusterName)
	}
	profile, ok := cluster.ConnectProfiles[profileKey]
	if !ok {
		return nil, fmt.Errorf("unknown connect profile %q for physical cluster %q", profileName, clusterName)
	}
	cacheKey := clusterKey + "/" + profileKey
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	if existing, ok := s.clients[cacheKey]; ok {
		return existing, nil
	}
	client, err := connect.NewClient(profile.REST)
	if err != nil {
		return nil, fmt.Errorf("create Connect REST client for %s: %w", cacheKey, err)
	}
	s.clients[cacheKey] = client
	return client, nil
}
