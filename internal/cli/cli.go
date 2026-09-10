package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/example/connectorctl/internal/workflow"
)

var version = "dev"

type globalOptions struct {
	configDir  string
	repoRoot   string
	reportsDir string
	renderDir  string
	kubectlBin string
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error {
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			*s = append(*s, item)
		}
	}
	return nil
}

// Execute parses command-line arguments and runs connectorctl.
func Execute() error {
	return execute(os.Args[1:], os.Stdout, os.Stderr)
}

func execute(args []string, stdout, stderr io.Writer) error {
	globalFS := flag.NewFlagSet("connectorctl", flag.ContinueOnError)
	globalFS.SetOutput(stderr)
	global := globalOptions{}
	globalFS.StringVar(&global.configDir, "config-dir", "config", "directory containing platforms.yaml, plugins.yaml, policies.yaml and secret-profiles.yaml")
	globalFS.StringVar(&global.repoRoot, "repo-root", ".", "root of the Git working tree")
	globalFS.StringVar(&global.reportsDir, "reports-dir", "reports", "directory for JSON run reports; empty disables report files")
	globalFS.StringVar(&global.renderDir, "render-dir", "rendered", "directory for dry-run CR previews; empty disables preview files")
	globalFS.StringVar(&global.kubectlBin, "kubectl-bin", "oc", "kubectl-compatible command used for server dry-run and annotations")
	globalFS.Usage = func() { printRootUsage(stderr) }
	if err := globalFS.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	remaining := globalFS.Args()
	if len(remaining) == 0 {
		printRootUsage(stderr)
		return errors.New("a command is required")
	}
	command := strings.ToLower(remaining[0])
	commandArgs := remaining[1:]
	if command == "help" || command == "-h" || command == "--help" {
		printRootUsage(stdout)
		return nil
	}
	if command == "version" {
		_, err := fmt.Fprintln(stdout, version)
		return err
	}

	service, err := workflow.NewService(global.configDir, global.repoRoot, global.reportsDir, global.kubectlBin, global.renderDir)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var commandErr error
	switch command {
	case "config-validate", "validate-config":
		if len(commandArgs) != 0 {
			return fmt.Errorf("config-validate accepts no command flags")
		}
		commandErr = writeJSON(stdout, map[string]any{"status": "valid", "configDir": global.configDir})
	case "repo-validate", "validate-repo":
		if len(commandArgs) != 0 {
			return fmt.Errorf("repo-validate accepts no command flags")
		}
		result, runErr := service.ValidateRepository()
		if err := writeJSON(stdout, result); err != nil {
			return err
		}
		commandErr = runErr
	case "adopt":
		commandErr = runAdopt(ctx, service, commandArgs, stdout, stderr)
	case "create":
		commandErr = runCreateOrUpdate(ctx, service, commandArgs, stdout, stderr, false)
	case "update":
		commandErr = runCreateOrUpdate(ctx, service, commandArgs, stdout, stderr, true)
	case "delete":
		commandErr = runDelete(ctx, service, commandArgs, stdout, stderr)
	case "migrate":
		commandErr = runMigrate(ctx, service, commandArgs, stdout, stderr)
	case "inventory":
		commandErr = runInventory(ctx, service, commandArgs, stdout, stderr)
	case "verify":
		commandErr = runVerify(ctx, service, commandArgs, stdout, stderr)
	case "ops", "operate":
		commandErr = runOps(ctx, service, commandArgs, stdout, stderr)
	default:
		printRootUsage(stderr)
		return fmt.Errorf("unknown command %q", command)
	}
	if errors.Is(commandErr, flag.ErrHelp) {
		return nil
	}
	return commandErr
}

func printRootUsage(w io.Writer) {
	fmt.Fprintln(w, `connectorctl - Kafka Connect to CFK GitOps migration utility

Usage:
  connectorctl [global flags] <command> [command flags]

Global flags must appear before the command:
  --config-dir string    Registry directory (default "config")
  --repo-root string     Git working-tree root (default ".")
  --reports-dir string   JSON report directory (default "reports")
  --render-dir string    Dry-run CR preview directory (default "rendered")
  --kubectl-bin string   kubectl-compatible binary (default "oc")

Commands:
  config-validate  Strictly validate all registry files
  repo-validate    Validate generated CR layout, identity and secret references
  inventory        Compare live Connect names with Git-managed CRs
  adopt            Download and convert existing live connectors
  create           Convert new JSON connector configs into CRs
  update           Update already managed CRs from JSON input
  migrate          Clone Git-managed connectors to a new environment
  delete           Remove managed connector CR files from Git
  verify           Verify live connector/task RUNNING states
  ops              status, pause, resume, restart or restart-task
  version           Print version

Mutating commands are dry-run by default. Pass --write to change the Git worktree
or execute an operational annotation.`)
}

func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(value)
}

func requireNonEmpty(values map[string]string) error {
	for name, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("--%s is required", name)
		}
	}
	return nil
}

func emitResult(stdout io.Writer, value any, reportPath string, runErr error) error {
	payload := map[string]any{"result": value}
	if reportPath != "" {
		payload["reportPath"] = reportPath
	}
	if runErr != nil {
		payload["error"] = runErr.Error()
	}
	if err := writeJSON(stdout, payload); err != nil {
		return err
	}
	return runErr
}
