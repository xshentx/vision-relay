package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLegacyProviderHealthCheckConfigIsIgnored(t *testing.T) {
	var loaded config
	if err := json.Unmarshal([]byte(`{
		"provider_health_check_enabled": true,
		"provider_health_check_interval_seconds": 60,
		"provider_failover_enabled": true
	}`), &loaded); err != nil {
		t.Fatal(err)
	}
	merged := mergeConfig(defaultConfig(), loaded)
	if !providerFailoverEnabled(merged) {
		t.Fatal("unrelated failover setting was not preserved")
	}
	encoded, err := json.Marshal(merged)
	if err != nil {
		t.Fatal(err)
	}
	for _, obsolete := range []string{"provider_health_check_enabled", "provider_health_check_interval_seconds"} {
		if strings.Contains(string(encoded), obsolete) {
			t.Fatalf("obsolete health-check setting is still persisted: %s", encoded)
		}
	}
}
