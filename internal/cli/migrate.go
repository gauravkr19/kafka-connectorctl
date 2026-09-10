package cli

import (
	"context"
	"flag"
	"io"

	"github.com/example/connectorctl/internal/workflow"
)

func runMigrate(ctx context.Context, service *workflow.Service, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var sourceCluster, sourceEnv, targetCluster, targetEnv, planPath, ticket string
	var concurrency int
	var selection selectionFlags
	var write, validateLive, serverDryRun, checkLive bool
	fs.StringVar(&sourceCluster, "source-cluster", "", "source physical cluster key")
	fs.StringVar(&sourceEnv, "source-env", "", "source logical environment")
	fs.StringVar(&targetCluster, "target-cluster", "", "target physical cluster key")
	fs.StringVar(&targetEnv, "target-env", "", "target logical environment")
	fs.StringVar(&planPath, "plan", "", "optional YAML migration plan with explicit replacements")
	fs.StringVar(&ticket, "ticket", "", "change-ticket identifier recorded as an annotation")
	fs.IntVar(&concurrency, "concurrency", 0, "worker count; zero uses policy default")
	addSelectionFlags(fs, &selection)
	fs.BoolVar(&write, "write", false, "write generated target CR files into the Git worktree")
	fs.BoolVar(&validateLive, "validate-live", false, "call target plugin config validation API")
	fs.BoolVar(&serverDryRun, "server-dry-run", false, "run oc/kubectl apply --dry-run=server for generated CRs")
	fs.BoolVar(&checkLive, "check-live", true, "reject target names that already exist in any target Connect profile")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requireNonEmpty(map[string]string{"source-cluster": sourceCluster, "source-env": sourceEnv, "target-cluster": targetCluster, "target-env": targetEnv}); err != nil {
		return err
	}
	run, reportPath, runErr := service.Migrate(ctx, workflow.MigrateOptions{
		SourceCluster: sourceCluster, SourceEnv: sourceEnv, TargetCluster: targetCluster, TargetEnv: targetEnv,
		Selection: selection.options(), PlanPath: planPath, Concurrency: concurrency, DryRun: !write,
		ChangeTicket: ticket, ValidateLive: validateLive, ServerDryRun: serverDryRun, CheckLive: checkLive,
	})
	return emitResult(stdout, run, reportPath, runErr)
}
