package transform

import (
	"sort"
	"strings"
)

type Difference struct {
	Key      string `json:"key" yaml:"key"`
	Expected string `json:"expected,omitempty" yaml:"expected,omitempty"`
	Actual   string `json:"actual,omitempty" yaml:"actual,omitempty"`
	Reason   string `json:"reason" yaml:"reason"`
}

func AdoptionDiff(live, generated map[string]string, sensitiveKeys []string) []Difference {
	sensitive := make(map[string]struct{}, len(sensitiveKeys))
	for _, key := range sensitiveKeys {
		sensitive[key] = struct{}{}
	}
	keys := map[string]struct{}{}
	for key := range live {
		if key != "name" {
			keys[key] = struct{}{}
		}
	}
	for key := range generated {
		if key != "name" {
			keys[key] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	var differences []Difference
	for _, key := range ordered {
		lv, lok := live[key]
		gv, gok := generated[key]
		if _, isSensitive := sensitive[key]; isSensitive {
			if !gok || !IsReference(gv) {
				differences = append(differences, Difference{Key: key, Reason: "sensitive field does not contain an external reference"})
			}
			continue
		}
		// Kafka Connect commonly defaults tasks.max to 1 even when the user did
		// not persist it. CFK requires taskMax, so those forms are equivalent.
		if key == "tasks.max" && !lok && gok && gv == "1" {
			continue
		}
		if !lok {
			differences = append(differences, Difference{Key: key, Expected: gv, Reason: "absent from live configuration"})
			continue
		}
		if !gok {
			differences = append(differences, Difference{Key: key, Actual: lv, Reason: "absent from generated configuration"})
			continue
		}
		if lv != gv {
			differences = append(differences, Difference{Key: key, Expected: gv, Actual: lv, Reason: "value mismatch"})
		}
	}
	return differences
}

func IsReference(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}")
}
