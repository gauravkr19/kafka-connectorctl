package cli

import (
	"context"
	"flag"
	"io"

	"github.com/example/connectorctl/internal/workflow"
)

func runDelete(ctx context.Context, service *workflow.Service, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var target targetFlags
	var selection selectionFlags
	var write, confirm bool
	var ticket string
	addTargetFlags(fs, &target)
	addSelectionFlags(fs, &selection)
	fs.BoolVar(&write, "write", false, "remove selected CR files from the Git worktree")
	fs.BoolVar(&confirm, "confirm-delete", false, "confirm that deletion may cause ArgoCD/CFK to remove live connectors")
	fs.StringVar(&ticket, "ticket", "", "required change-ticket identifier when policy demands it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requireNonEmpty(map[string]string{"cluster": target.cluster, "env": target.logicalEnv}); err != nil {
		return err
	}
	run, reportPath, runErr := service.Delete(ctx, workflow.DeleteOptions{
		Cluster: target.cluster, LogicalEnv: target.logicalEnv, Selection: selection.options(), Concurrency: target.concurrency,
		DryRun: !write, ChangeTicket: ticket, Confirm: confirm,
	})
	return emitResult(stdout, run, reportPath, runErr)
}
