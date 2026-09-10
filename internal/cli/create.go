package cli

import (
	"context"
	"flag"
	"io"

	"github.com/example/connectorctl/internal/workflow"
)

func runCreateOrUpdate(ctx context.Context, service *workflow.Service, args []string, stdout, stderr io.Writer, update bool) error {
	name := "create"
	if update {
		name = "update"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var target targetFlags
	var inputPath, connectorName, ticket string
	var batchSize, batchOffset int
	var write, validateLive, serverDryRun, checkLive, unsafeAll bool
	addTargetFlags(fs, &target)
	fs.StringVar(&inputPath, "input", "", "JSON file or directory of JSON files")
	fs.StringVar(&connectorName, "connector-name", "", "connector name override for a single raw config JSON file")
	fs.IntVar(&batchSize, "batch-size", 0, "maximum JSON inputs to process in this run")
	fs.IntVar(&batchOffset, "batch-offset", 0, "zero-based offset into sorted JSON input names")
	fs.BoolVar(&unsafeAll, "unsafe-all", false, "process an unbounded input set; requires policy approval")
	fs.BoolVar(&write, "write", false, "write generated CR files into the Git worktree")
	fs.BoolVar(&validateLive, "validate-live", false, "call the target plugin config validation API")
	fs.BoolVar(&serverDryRun, "server-dry-run", false, "run oc/kubectl apply --dry-run=server for every generated CR")
	fs.BoolVar(&checkLive, "check-live", true, "verify connector existence/non-existence through Connect REST")
	fs.StringVar(&ticket, "ticket", "", "change-ticket identifier recorded as an annotation")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requireNonEmpty(map[string]string{"cluster": target.cluster, "env": target.logicalEnv, "input": inputPath}); err != nil {
		return err
	}
	opts := workflow.CreateOptions{
		Cluster: target.cluster, LogicalEnv: target.logicalEnv, InputPath: inputPath, Name: connectorName,
		Concurrency: target.concurrency, BatchSize: batchSize, BatchOffset: batchOffset, UnsafeAll: unsafeAll,
		DryRun: !write, ChangeTicket: ticket,
		ValidateLive: validateLive, ServerDryRun: serverDryRun, CheckLive: checkLive,
	}
	var runErr error
	var reportPath string
	var run any
	if update {
		r, path, err := service.Update(ctx, opts)
		run, reportPath, runErr = r, path, err
	} else {
		r, path, err := service.Create(ctx, opts)
		run, reportPath, runErr = r, path, err
	}
	return emitResult(stdout, run, reportPath, runErr)
}
