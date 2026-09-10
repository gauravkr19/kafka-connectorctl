package repository

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyBatchPreflightPreventsPartialWrite(t *testing.T) {
	root := t.TempDir()
	repo := New(root)
	first := filepath.Join(root, "first.yaml")
	second := filepath.Join(root, "second.yaml")
	if err := os.WriteFile(second, []byte("existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := repo.ApplyBatch([]Mutation{
		{Type: MutationWrite, Path: first, Content: []byte("new\n")},
		{Type: MutationWrite, Path: second, Content: []byte("replacement\n"), Overwrite: false},
	})
	if err == nil {
		t.Fatal("expected batch preflight failure")
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("first mutation was applied before second failed: %v", err)
	}
	data, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "existing\n" {
		t.Fatalf("existing file was modified: %q", data)
	}
}

func TestApplyBatchWritesAndRemoves(t *testing.T) {
	root := t.TempDir()
	repo := New(root)
	oldPath := filepath.Join(root, "old.yaml")
	newPath := filepath.Join(root, "new.yaml")
	if err := os.WriteFile(oldPath, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.ApplyBatch([]Mutation{
		{Type: MutationWrite, Path: newPath, Content: []byte("new\n")},
		{Type: MutationRemove, Path: oldPath},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old path still exists: %v", err)
	}
	data, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new\n" {
		t.Fatalf("unexpected new content: %q", data)
	}
}
