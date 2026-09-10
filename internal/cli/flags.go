package cli

import (
	"flag"

	"github.com/example/connectorctl/internal/workflow"
)

type targetFlags struct {
	cluster     string
	logicalEnv  string
	concurrency int
}

func addTargetFlags(fs *flag.FlagSet, target *targetFlags) {
	fs.StringVar(&target.cluster, "cluster", "", "physical Kafka/Connect cluster key")
	fs.StringVar(&target.logicalEnv, "env", "", "logical environment key")
	fs.IntVar(&target.concurrency, "concurrency", 0, "worker count; zero uses policy default")
}

type selectionFlags struct {
	names       stringList
	namesFile   string
	allEnv      bool
	batchSize   int
	batchOffset int
	unsafeAll   bool
}

func addSelectionFlags(fs *flag.FlagSet, selection *selectionFlags) {
	fs.Var(&selection.names, "name", "connector name; repeat or provide a comma-separated list")
	fs.StringVar(&selection.namesFile, "names-file", "", "newline or CSV file containing connector names")
	fs.BoolVar(&selection.allEnv, "all-env", false, "discover/select every connector matching the logical environment")
	fs.IntVar(&selection.batchSize, "batch-size", 0, "number of sorted connectors to process; zero applies policy defaults")
	fs.IntVar(&selection.batchOffset, "batch-offset", 0, "zero-based offset into the sorted selection")
	fs.BoolVar(&selection.unsafeAll, "unsafe-all", false, "permit a policy-approved unbounded whole-environment batch")
}

func (s selectionFlags) options() workflow.SelectionOptions {
	return workflow.SelectionOptions{
		Names:       append([]string(nil), s.names...),
		NamesFile:   s.namesFile,
		AllEnv:      s.allEnv,
		BatchSize:   s.batchSize,
		BatchOffset: s.batchOffset,
		UnsafeAll:   s.unsafeAll,
	}
}
