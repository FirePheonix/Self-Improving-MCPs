package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"self-improving-mcp/config"
)

func main() {
	_ = config.LoadDotEnv(".env")
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: evolve <evaluate|propose|sandbox|gate|promote|discard|rollback|versions|import|improve|upstream-patch|golden|run> [flags]\n")
		os.Exit(2)
	}
	cmd := os.Args[1]
	os.Args = append([]string{os.Args[0]}, os.Args[2:]...)

	ctx := context.Background()
	var err error
	switch cmd {
	case "evaluate":
		err = runEvaluate(ctx)
	case "propose":
		err = runPropose(ctx)
	case "sandbox":
		err = runSandbox(ctx)
	case "gate":
		err = runGate(ctx)
	case "promote":
		err = runPromote(ctx)
	case "discard":
		err = runDiscard(ctx)
	case "rollback":
		err = runRollback(ctx)
	case "versions":
		err = runVersions(ctx)
	case "improve":
		err = runImprove(ctx)
	case "upstream-patch":
		err = runUpstreamPatch(ctx)
	case "import":
		err = runImport(ctx)
	case "golden":
		err = runGolden(ctx)
	case "run":
		err = runLoop(ctx)
	default:
		err = fmt.Errorf("unknown command %q", cmd)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
