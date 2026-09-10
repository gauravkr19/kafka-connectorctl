package cli

import (
	"context"
	"flag"
	"io"

	"github.com/example/connectorctl/internal/workflow"
)

func runAdopt(ctx context.Context, service *workflow.Service, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("adopt", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var target targetFlags
	var selection selectionFlags
	var write, validateLive, serverDryRun bool
	var ticket, sourceProfile string
	addTargetFlags(fs, &target)
	addSelectionFlags(fs, &selection)
	fs.BoolVar(&write, "write", false, "write generated CR files into the Git worktree")
	fs.BoolVar(&validateLive, "validate-live", false, "call the target plugin config validation API")
	fs.BoolVar(&serverDryRun, "server-dry-run", false, "run oc/kubectl apply --dry-run=server for every generated CR")
	fs.StringVar(&ticket, "ticket", "", "change-ticket identifier recorded as an annotation")
	fs.StringVar(&sourceProfile, "source-profile", "", "restrict discovery to one Connect profile; empty uses adoptionSource profiles")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requireNonEmpty(map[string]string{"cluster": target.cluster, "env": target.logicalEnv}); err != nil {
		return err
	}
	run, reportPath, runErr := service.Adopt(ctx, workflow.AdoptOptions{
		Cluster: target.cluster, LogicalEnv: target.logicalEnv, SourceProfile: sourceProfile, Selection: selection.options(), Concurrency: target.concurrency,
		DryRun: !write, ChangeTicket: ticket, ValidateLive: validateLive, ServerDryRun: serverDryRun,
	})
	return emitResult(stdout, run, reportPath, runErr)
}
