package workflow

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"

	"github.com/example/connectorctl/internal/model"
)

func runConcurrent(ctx context.Context, names []string, workers int, fn func(context.Context, string) model.ItemResult) []model.ItemResult {
	if workers < 1 {
		workers = 1
	}
	if workers > len(names) && len(names) > 0 {
		workers = len(names)
	}
	jobs := make(chan string)
	results := make(chan model.ItemResult, len(names))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range jobs {
				if err := ctx.Err(); err != nil {
					results <- model.ItemResult{Name: name, Status: "failed", Error: err.Error()}
					continue
				}
				results <- safeItemCall(ctx, name, fn)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, name := range names {
			jobs <- name
		}
	}()
	wg.Wait()
	close(results)
	out := make([]model.ItemResult, 0, len(names))
	for result := range results {
		out = append(out, result)
	}
	return out
}

func safeItemCall(ctx context.Context, name string, fn func(context.Context, string) model.ItemResult) (result model.ItemResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = model.ItemResult{
				Name: name, Status: "failed",
				Error:    fmt.Sprintf("internal panic while processing connector: %v", recovered),
				Warnings: []string{"stack trace suppressed from standard output; inspect Jenkins/controller logs if debugging is enabled"},
			}
			_ = debug.Stack() // Keep runtime stack generation available to debuggers without exposing connector data.
		}
	}()
	return fn(ctx, name)
}
