package repository

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

type MutationType string

const (
	MutationWrite  MutationType = "write"
	MutationRemove MutationType = "remove"
)

// Mutation describes one worktree change. Callers should preflight mutations
// with PlanWrite or PlanRemove before applying a batch.
type Mutation struct {
	Type      MutationType
	Path      string
	Content   []byte
	Overwrite bool
}

type backup struct {
	path    string
	existed bool
	content []byte
	mode    fs.FileMode
}

func (r *Repository) PlanWrite(path string, content []byte, overwrite bool) (bool, error) {
	current, err := os.ReadFile(path)
	if err == nil {
		if string(current) == string(content) {
			return false, nil
		}
		if !overwrite {
			return false, fmt.Errorf("file already exists and overwrite is disabled: %s", path)
		}
		return true, nil
	}
	if os.IsNotExist(err) {
		return true, nil
	}
	return false, err
}

func (r *Repository) PlanRemove(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("connector manifest does not exist: %s", path)
		}
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("refusing to remove directory as connector manifest: %s", path)
	}
	return nil
}

// ApplyBatch preflights every mutation before touching the worktree. If an
// apply step fails, it restores every path already changed in this batch.
func (r *Repository) ApplyBatch(mutations []Mutation) error {
	if len(mutations) == 0 {
		return nil
	}
	ordered := append([]Mutation(nil), mutations...)
	sort.SliceStable(ordered, func(i, j int) bool {
		// Write destinations before removing old paths. This makes class/path
		// moves safer and rollback remains available for either operation.
		if ordered[i].Type != ordered[j].Type {
			return ordered[i].Type == MutationWrite
		}
		return filepath.Clean(ordered[i].Path) < filepath.Clean(ordered[j].Path)
	})
	seen := map[string]MutationType{}
	backups := make(map[string]backup, len(ordered))
	for _, mutation := range ordered {
		path := filepath.Clean(mutation.Path)
		if path == "." || path == string(filepath.Separator) {
			return fmt.Errorf("invalid mutation path %q", mutation.Path)
		}
		if previous, exists := seen[path]; exists {
			return fmt.Errorf("multiple batch mutations target %s (%s and %s)", path, previous, mutation.Type)
		}
		seen[path] = mutation.Type
		switch mutation.Type {
		case MutationWrite:
			changed, err := r.PlanWrite(path, mutation.Content, mutation.Overwrite)
			if err != nil {
				return err
			}
			if !changed {
				continue
			}
		case MutationRemove:
			if err := r.PlanRemove(path); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported mutation type %q", mutation.Type)
		}
		b, err := captureBackup(path)
		if err != nil {
			return err
		}
		backups[path] = b
	}

	var applied []string
	for _, mutation := range ordered {
		path := filepath.Clean(mutation.Path)
		if _, changed := backups[path]; !changed {
			continue
		}
		var err error
		switch mutation.Type {
		case MutationWrite:
			_, err = r.WriteAtomic(path, mutation.Content, true)
		case MutationRemove:
			err = r.Remove(path)
		}
		if err != nil {
			rollbackErr := rollback(r, applied, backups)
			if rollbackErr != nil {
				return fmt.Errorf("apply batch at %s: %w; rollback also failed: %v", path, err, rollbackErr)
			}
			return fmt.Errorf("apply batch at %s: %w; prior changes were rolled back", path, err)
		}
		applied = append(applied, path)
	}
	return nil
}

func captureBackup(path string) (backup, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return backup{path: path}, nil
		}
		return backup{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return backup{}, err
	}
	return backup{path: path, existed: true, content: data, mode: info.Mode().Perm()}, nil
}

func rollback(r *Repository, applied []string, backups map[string]backup) error {
	var first error
	for i := len(applied) - 1; i >= 0; i-- {
		b := backups[applied[i]]
		if b.existed {
			if _, err := r.WriteAtomic(b.path, b.content, true); err != nil && first == nil {
				first = err
				continue
			}
			if err := os.Chmod(b.path, b.mode); err != nil && first == nil {
				first = err
			}
			continue
		}
		if err := os.Remove(b.path); err != nil && !os.IsNotExist(err) && first == nil {
			first = err
		}
	}
	return first
}
