package cli

import (
	"context"
	"flag"
	"io"

	"github.com/example/connectorctl/internal/workflow"
)

func runVerify(ctx context.Context, service *workflow.Service, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var target targetFlags
	var selection selectionFlags
	addTargetFlags(fs, &target)
	addSelectionFlags(fs, &selection)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requireNonEmpty(map[string]string{"cluster": target.cluster, "env": target.logicalEnv}); err != nil {
		return err
	}
	run, reportPath, runErr := service.Verify(ctx, workflow.VerifyOptions{
		Cluster: target.cluster, LogicalEnv: target.logicalEnv, Selection: selection.options(), Concurrency: target.concurrency,
	})
	return emitResult(stdout, run, reportPath, runErr)
}
