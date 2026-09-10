package repository

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type ParsedConfig struct {
	Name       string
	Config     map[string]string
	SourcePath string
	Warnings   []string
}

func LoadJSONInputs(inputPath, explicitName string) ([]ParsedConfig, error) {
	info, err := os.Stat(inputPath)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		parsed, err := parseJSONFile(inputPath, explicitName)
		if err != nil {
			return nil, err
		}
		return []ParsedConfig{parsed}, nil
	}
	entries, err := os.ReadDir(inputPath)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			paths = append(paths, filepath.Join(inputPath, entry.Name()))
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no JSON files found under %s", inputPath)
	}
	out := make([]ParsedConfig, 0, len(paths))
	for _, path := range paths {
		p, err := parseJSONFile(path, "")
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// parseJSONFile loads one connector JSON config.
// Connector name precedence is: explicit name -> wrapper "name" -> filename.
// Both raw config JSON and {"name": "...", "config": {...}} formats are supported.
func parseJSONFile(path, explicitName string) (ParsedConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ParsedConfig{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return ParsedConfig{}, fmt.Errorf("parse %s: %w", path, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != nil && err != io.EOF {
		return ParsedConfig{}, fmt.Errorf("parse trailing data in %s: %w", path, err)
	} else if err == nil {
		return ParsedConfig{}, fmt.Errorf("parse %s: multiple JSON values are not supported", path)
	}
	name := strings.TrimSpace(explicitName)
	cfgRaw := raw
	if wrapper, ok := raw["config"].(map[string]any); ok {
		cfgRaw = wrapper
		if n, ok := raw["name"].(string); ok && strings.TrimSpace(n) != "" {
			wrapperName := strings.TrimSpace(n)
			if name != "" && name != wrapperName {
				return ParsedConfig{}, fmt.Errorf("%s: explicit connector name %q does not match wrapper name %q", path, name, wrapperName)
			}
			name = wrapperName
		}
	}
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	cfg := make(map[string]string, len(cfgRaw))
	var warnings []string
	for key, value := range cfgRaw {
		s, warning, err := stringify(value)
		if err != nil {
			return ParsedConfig{}, fmt.Errorf("%s key %q: %w", path, key, err)
		}
		cfg[key] = s
		if warning != "" {
			warnings = append(warnings, key+": "+warning)
		}
	}
	return ParsedConfig{Name: name, Config: cfg, SourcePath: path, Warnings: warnings}, nil
}

func stringify(value any) (string, string, error) {
	switch v := value.(type) {
	case string:
		return v, "", nil
	case json.Number:
		return v.String(), "numeric JSON value normalized to string", nil
	case bool:
		return strconv.FormatBool(v), "boolean JSON value normalized to string", nil
	case nil:
		return "", "", fmt.Errorf("null is not a valid Kafka Connect string value")
	case map[string]any, []any:
		data, err := json.Marshal(v)
		return string(data), "structured JSON compacted into a JSON string", err
	default:
		return fmt.Sprint(v), "value normalized to string", nil
	}
}
