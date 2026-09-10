package cli

import (
	"context"
	"flag"
	"io"

	"github.com/example/connectorctl/internal/workflow"
)

func runInventory(ctx context.Context, service *workflow.Service, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("inventory", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var cluster, logicalEnv string
	fs.StringVar(&cluster, "cluster", "", "physical Kafka/Connect cluster key")
	fs.StringVar(&logicalEnv, "env", "", "logical environment key")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requireNonEmpty(map[string]string{"cluster": cluster, "env": logicalEnv}); err != nil {
		return err
	}
	result, err := service.Inventory(ctx, cluster, logicalEnv)
	if err != nil {
		return err
	}
	return writeJSON(stdout, result)
}
