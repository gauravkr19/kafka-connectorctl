package workflow

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type SelectionOptions struct {
	Names       []string
	NamesFile   string
	AllEnv      bool
	BatchSize   int
	BatchOffset int
	UnsafeAll   bool
}

// selectNames combines both sources into one deduplicated set:
func selectNames(allLive []string, envRegex string, opts SelectionOptions, allowWholeEnv bool, defaultBatch, maxBatch int) ([]string, error) {
	re, err := regexp.Compile(envRegex)
	if err != nil {
		return nil, err
	}
	set := map[string]struct{}{}

	for _, name := range opts.Names {
		if strings.TrimSpace(name) != "" {
			set[strings.TrimSpace(name)] = struct{}{}
		}
	}
	if opts.NamesFile != "" {
		names, err := readNamesFile(opts.NamesFile)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			set[name] = struct{}{}
		}
	}

	if opts.AllEnv {
		if !allowWholeEnv {
			return nil, fmt.Errorf("whole-environment selection is disabled by policy")
		}
		for _, name := range allLive {
			if re.MatchString(name) {
				set[name] = struct{}{}
			}
		}
	}

	if len(set) == 0 {
		return nil, fmt.Errorf("no connectors selected")
	}
	selected := make([]string, 0, len(set))
	for name := range set {
		if !re.MatchString(name) {
			return nil, fmt.Errorf("connector %q does not match the configured logical-environment naming rule %q", name, envRegex)
		}
		selected = append(selected, name)
	}
	sort.Strings(selected)
	batch := opts.BatchSize
	if opts.AllEnv && batch == 0 && !opts.UnsafeAll {
		batch = defaultBatch
	}
	return applyBatchLimits(selected, batch, opts.BatchOffset, opts.UnsafeAll, allowWholeEnv, maxBatch)
}

// applyBatchLimits enforces the configured blast-radius limit for every bulk
// workflow, not only whole-environment selection. An explicit CSV containing
// hundreds of names must therefore opt into bounded pages or an authorized
// unsafe-all run.
func applyBatchLimits(names []string, batchSize, batchOffset int, unsafeAll, allowUnsafeAll bool, maxBatch int) ([]string, error) {
	if batchSize < 0 {
		return nil, fmt.Errorf("batch size cannot be negative")
	}
	if batchOffset < 0 {
		return nil, fmt.Errorf("batch offset cannot be negative")
	}
	if unsafeAll && !allowUnsafeAll {
		return nil, fmt.Errorf("unsafe unbounded processing is disabled by policy")
	}
	if batchSize > maxBatch && !unsafeAll {
		return nil, fmt.Errorf("batch size %d exceeds policy maximum %d", batchSize, maxBatch)
	}
	if batchSize == 0 && len(names) > maxBatch && !unsafeAll {
		return nil, fmt.Errorf("selected connector count %d exceeds policy maximum %d; use --batch-size and --batch-offset", len(names), maxBatch)
	}
	if batchOffset > len(names) || batchOffset == len(names) && len(names) > 0 {
		return nil, fmt.Errorf("batch offset %d is outside selected connector count %d", batchOffset, len(names))
	}
	selected := names[batchOffset:]
	if batchSize > 0 && len(selected) > batchSize {
		selected = selected[:batchSize]
	}
	return selected, nil
}

// readNamesFile loads explicit ADOPT connector names.
// Supports one name per text line or one name per CSV row (first column).
func readNamesFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil, fmt.Errorf("names file %s is empty", path)
	}
	if strings.EqualFold(filepath.Ext(path), ".csv") || strings.Contains(text, ",") {
		r := csv.NewReader(strings.NewReader(text))
		records, err := r.ReadAll()
		if err != nil {
			return nil, err
		}
		var names []string
		for i, row := range records {
			if len(row) == 0 {
				continue
			}
			value := strings.TrimSpace(strings.TrimPrefix(row[0], "\ufeff"))
			if i == 0 && strings.EqualFold(value, "connector_name") {
				continue
			}
			if value != "" {
				names = append(names, value)
			}
		}
		return names, nil
	}
	var names []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			names = append(names, line)
		}
	}
	return names, nil
}

// validateAdoptSelection is a guardrail leading to adopt unwanted connectors
func validateAdoptSelection(opts SelectionOptions) error {
	if opts.AllEnv {
		return fmt.Errorf("adopt requires explicit connector names; --all-env is not allowed")
	}
	if opts.UnsafeAll {
		return fmt.Errorf("--unsafe-all is not supported for adopt")
	}
	if len(opts.Names) == 0 && strings.TrimSpace(opts.NamesFile) == "" {
		return fmt.Errorf("adopt requires at least one --name or --names-file")
	}
	return nil
}
