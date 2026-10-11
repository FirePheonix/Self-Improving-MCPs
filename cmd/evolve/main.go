package main

import (
	"context"
	"fmt"
	"log"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: evolve <evaluate|propose|sandbox|promote|discard|rollback|versions|run> [flags]\n")
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
	case "promote":
		err = runPromote(ctx)
	case "discard":
		err = runDiscard(ctx)
	case "rollback":
		err = runRollback(ctx)
	case "versions":
		err = runVersions(ctx)
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
