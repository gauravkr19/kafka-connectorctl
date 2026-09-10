package registry

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/example/connectorctl/internal/config"
)

type Entry struct {
	Alias             string
	Rule              config.PluginRule
	sensitivePatterns []*regexp.Regexp
}

type Registry struct {
	byClass         map[string]Entry
	genericPatterns []*regexp.Regexp
}

func New(plugins config.PluginsFile, genericPatterns []string) (*Registry, error) {
	r := &Registry{byClass: map[string]Entry{}}
	for _, pattern := range genericPatterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("compile generic sensitive pattern %q: %w", pattern, err)
		}
		r.genericPatterns = append(r.genericPatterns, re)
	}
	for alias, rule := range plugins.Plugins {
		entry := Entry{Alias: alias, Rule: rule}
		for _, pattern := range rule.SensitivePatterns {
			re, err := regexp.Compile(pattern)
			if err != nil {
				return nil, fmt.Errorf("compile plugin %s sensitive pattern %q: %w", alias, pattern, err)
			}
			entry.sensitivePatterns = append(entry.sensitivePatterns, re)
		}
		for _, className := range rule.Classes {
			r.byClass[className] = entry
		}
	}
	return r, nil
}

func (r *Registry) Lookup(className string) (Entry, error) {
	entry, ok := r.byClass[strings.TrimSpace(className)]
	if !ok {
		return Entry{}, fmt.Errorf("connector class %q is not registered", className)
	}
	return entry, nil
}

func (r *Registry) Validate(entry Entry, cfg map[string]string) error {
	var missing []string
	for _, key := range entry.Rule.Required {
		if strings.TrimSpace(cfg[key]) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("plugin %s is missing required configuration keys: %s", entry.Alias, strings.Join(missing, ", "))
	}
	return nil
}

func (r *Registry) SensitiveKeys(entry Entry, cfg map[string]string) []string {
	exact := map[string]struct{}{}
	for _, key := range entry.Rule.Sensitive {
		exact[key] = struct{}{}
	}
	var found []string
	for key := range cfg {
		_, isExact := exact[key]
		if isExact || matchesAny(key, entry.sensitivePatterns) || matchesAny(key, r.genericPatterns) {
			found = append(found, key)
		}
	}
	sort.Strings(found)
	return found
}

func matchesAny(value string, patterns []*regexp.Regexp) bool {
	for _, re := range patterns {
		if re.MatchString(value) {
			return true
		}
	}
	return false
}
