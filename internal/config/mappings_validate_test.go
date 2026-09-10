package config

import "testing"

func TestValidateSecretMappings(t *testing.T) {
	profiles := SecretProfilesFile{Profiles: map[string]SecretProfile{
		"mounted-file": {Type: "kubernetes-mounted-secret", MountRoot: "/mnt/secrets", DefaultFile: "custom.properties"},
	}}
	valid := SecretMappingsFile{APIVersion: RegistryAPIVersion, Connectors: map[string]ConnectorMapping{
		"ngil-sit-db": {Fields: map[string]SecretFieldRef{
			"database.password": {SecretName: "sit-db", SecretKey: "password"},
		}},
	}}
	if err := ValidateSecretMappings(valid, profiles); err != nil {
		t.Fatalf("valid mapping rejected: %v", err)
	}
	invalid := SecretMappingsFile{APIVersion: RegistryAPIVersion, Connectors: map[string]ConnectorMapping{
		"ngil-sit-db": {Fields: map[string]SecretFieldRef{
			"database.password": {SecretName: "../bad", SecretKey: "bad:key"},
		}},
	}}
	if err := ValidateSecretMappings(invalid, profiles); err == nil {
		t.Fatal("expected invalid mapping to fail")
	}
}
