package transform

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/example/connectorctl/internal/config"
	"github.com/example/connectorctl/internal/model"
	"github.com/example/connectorctl/internal/yaml"
)

type Renderer struct {
	APIVersion string
	Kind       string
}

func NewRenderer() *Renderer {
	return &Renderer{APIVersion: "platform.confluent.io/v1beta1", Kind: "Connector"}
}

func Normalize(connectorName, physicalCluster, logicalEnv, pluginAlias, origin, changeTicket string, cfg map[string]string, connectProfile string) (model.NormalizedConnector, error) {
	connectorName = strings.TrimSpace(connectorName)
	if connectorName == "" {
		return model.NormalizedConnector{}, fmt.Errorf("connector name is required")
	}
	if strings.ContainsAny(connectorName, "\x00\r\n\t") {
		return model.NormalizedConnector{}, fmt.Errorf("connector name contains a control character")
	}
	if strings.ContainsAny(changeTicket, "\x00\r\n") {
		return model.NormalizedConnector{}, fmt.Errorf("change ticket contains a control character")
	}
	className := strings.TrimSpace(cfg["connector.class"])
	if className == "" {
		return model.NormalizedConnector{}, fmt.Errorf("connector %s contains no connector.class", connectorName)
	}
	taskMax := 1
	if raw := strings.TrimSpace(cfg["tasks.max"]); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return model.NormalizedConnector{}, fmt.Errorf("connector %s has invalid tasks.max %q", connectorName, raw)
		}
		taskMax = parsed
	}
	configs := make(map[string]string, len(cfg))
	for k, v := range cfg {
		if strings.TrimSpace(k) == "" || strings.ContainsAny(k, "\x00\r\n\t") {
			return model.NormalizedConnector{}, fmt.Errorf("connector %s contains an invalid configuration key", connectorName)
		}
		switch k {
		case "connector.class", "tasks.max", "name":
			continue
		default:
			configs[k] = v
		}
	}
	return model.NormalizedConnector{Name: connectorName, PhysicalCluster: strings.ToLower(physicalCluster), LogicalEnv: strings.ToLower(logicalEnv), PluginAlias: strings.ToLower(pluginAlias), Class: className, TaskMax: taskMax, ConnectProfile: connectProfile, Origin: origin, ChangeTicket: changeTicket, Configs: configs}, nil
}

func (r *Renderer) Build(n model.NormalizedConnector, cluster config.Cluster) (model.CFKConnector, error) {
	profile, ok := cluster.ConnectProfiles[n.ConnectProfile]
	if !ok {
		return model.CFKConnector{}, fmt.Errorf("connect profile %q is not configured for cluster %s", n.ConnectProfile, n.PhysicalCluster)
	}
	restart := model.RestartPolicy{Type: "OnFailure", MaxRetry: 10}
	if profile.RestartPolicy != nil {
		restart = *profile.RestartPolicy
	}
	labels := map[string]string{"app.kubernetes.io/managed-by": "connectorctl", "connectorctl.io/physical-cluster": n.PhysicalCluster, "connectorctl.io/logical-env": n.LogicalEnv, "connectorctl.io/connector-plugin": n.PluginAlias, "connectorctl.io/connect-profile": n.ConnectProfile}
	annotations := map[string]string{"connectorctl.io/origin": n.Origin}
	if n.ChangeTicket != "" {
		annotations["connectorctl.io/change-ticket"] = n.ChangeTicket
	}
	return model.CFKConnector{APIVersion: r.APIVersion, Kind: r.Kind, Metadata: model.ObjectMeta{Name: KubernetesName(n.Name), Namespace: cluster.Namespace, Labels: labels, Annotations: annotations}, Spec: model.ConnectorSpec{Name: n.Name, Class: n.Class, TaskMax: n.TaskMax, ConnectClusterRef: profile.ConnectClusterRef, ConnectRest: cloneAnyMap(profile.ConnectRest), Configs: n.Configs, RestartPolicy: restart}}, nil
}

func (r *Renderer) Marshal(cr model.CFKConnector) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cr); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
