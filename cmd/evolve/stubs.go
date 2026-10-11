package main

import (
	"context"
	"fmt"
)

func runSandbox(ctx context.Context) error {
	_ = ctx
	return fmt.Errorf("sandbox not wired yet")
}

func runPromote(ctx context.Context) error {
	_ = ctx
	return fmt.Errorf("promote not wired yet")
}

func runDiscard(ctx context.Context) error {
	_ = ctx
	return fmt.Errorf("discard not wired yet")
}

func runRollback(ctx context.Context) error {
	_ = ctx
	return fmt.Errorf("rollback not wired yet")
}

func runVersions(ctx context.Context) error {
	_ = ctx
	return fmt.Errorf("versions not wired yet")
}

func runLoop(ctx context.Context) error {
	_ = ctx
	return fmt.Errorf("run not wired yet")
}
