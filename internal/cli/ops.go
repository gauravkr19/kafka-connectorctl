package cli

import (
	"context"
	"flag"
	"io"

	"github.com/example/connectorctl/internal/workflow"
)

func runOps(ctx context.Context, service *workflow.Service, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("ops", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var target targetFlags
	var selection selectionFlags
	var operation string
	var taskID int
	var write bool
	addTargetFlags(fs, &target)
	addSelectionFlags(fs, &selection)
	fs.StringVar(&operation, "operation", "status", "status, pause, resume, restart or restart-task")
	fs.IntVar(&taskID, "task-id", -1, "task ID for restart-task")
	fs.BoolVar(&write, "write", false, "execute the annotation; omitted means dry-run")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requireNonEmpty(map[string]string{"cluster": target.cluster, "env": target.logicalEnv, "operation": operation}); err != nil {
		return err
	}
	run, reportPath, runErr := service.Operate(ctx, workflow.OpsOptions{
		Cluster: target.cluster, LogicalEnv: target.logicalEnv, Operation: operation,
		Selection: selection.options(), Concurrency: target.concurrency, TaskID: taskID, DryRun: !write,
	})
	return emitResult(stdout, run, reportPath, runErr)
}
