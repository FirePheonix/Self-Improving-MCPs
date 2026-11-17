package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	"self-improving-mcp/agent"
	"self-improving-mcp/mcp"
	selectiongate "self-improving-mcp/selection-gate"
)

// runGolden implements `evolve golden <draft|approve>`: build a sealed test set for a server
// from its own tool docs, with a person approving every task before the gate may use it.
func runGolden(ctx context.Context) error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: evolve golden <draft|approve> [flags]")
	}
	sub := os.Args[1]
	os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
	switch sub {
	case "draft":
		return goldenDraft(ctx)
	case "approve":
		return goldenApprove(ctx)
	}
	return fmt.Errorf("unknown golden subcommand %q (want draft or approve)", sub)
}

func goldenDraft(ctx context.Context) error {
	fs := flag.NewFlagSet("golden draft", flag.ContinueOnError)
	cfgPath := fs.String("config", envOr("EVOLVE_CONFIG", "config/evolve.yaml"), "path to evolve yaml")
	proxyCfgPath := fs.String("proxy-config", envOr("PROXY_CONFIG", "config/proxy.yaml"), "proxy config for upstream")
	out := fs.String("out", "", "draft file to write (required)")
	name := fs.String("name", "golden", "name recorded in the set")
	perTool := fs.Int("per-tool", 4, "tasks to draft per tool")
	noBaseline := fs.Bool("no-baseline", false, "skip recording the current selector pick for each task")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *out == "" {
		return fmt.Errorf("-out is required")
	}
	if _, err := os.Stat(*out); err == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite a draft that may hold review work", *out)
	}
	cfg, err := loadEvolveConfig(*cfgPath)
	if err != nil {
		return err
	}
	proxyCfg, err := loadProxyConfig(*proxyCfgPath)
	if err != nil {
		return err
	}
	upstream, err := mcp.ConnectStdio(ctx, proxyCfg.Upstream)
	if err != nil {
		return err
	}
	defer upstream.Close()
	live, err := upstream.ListTools(ctx)
	if err != nil {
		return err
	}
	tools := toGateTools(live)

	model := cfg.Gate.Model
	if model == "" {
		model = cfg.Models.Reason
	}
	if model == "" {
		model = "gpt-4.1-mini"
	}
	client, err := agent.NewClient(cfg.Models.Label, model)
	if err != nil {
		return err
	}
	llm := func(ctx context.Context, system, user string) (string, error) {
		return client.CompleteJSON(ctx, model, system, user)
	}
	draft, dropped, err := selectiongate.GenerateDraft(ctx, llm, tools, selectiongate.DraftOptions{Name: *name, Model: model, PerTool: *perTool})
	for _, d := range dropped {
		fmt.Println("dropped:", d)
	}
	if err != nil {
		return err
	}

	mismatches := 0
	if !*noBaseline {
		zero := 0.0
		client.Temperature = &zero
		sel := agent.OpenAISelector{Client: client, Model: model}
		for i := range draft.Tasks {
			pick, err := sel.Select(ctx, draft.Tasks[i].Query, tools)
			if err != nil {
				fmt.Printf("baseline pick failed for %s: %v\n", draft.Tasks[i].ID, err)
				continue
			}
			draft.Tasks[i].BaselinePick = pick
			if pick != draft.Tasks[i].ExpectedTool {
				mismatches++
			}
		}
		// Review the disagreements first: they are either real confusions or wrong labels.
		sort.SliceStable(draft.Tasks, func(i, j int) bool {
			mi := draft.Tasks[i].BaselinePick != "" && draft.Tasks[i].BaselinePick != draft.Tasks[i].ExpectedTool
			mj := draft.Tasks[j].BaselinePick != "" && draft.Tasks[j].BaselinePick != draft.Tasks[j].ExpectedTool
			return mi && !mj
		})
	}
	if err := draft.Save(*out); err != nil {
		return err
	}
	fmt.Printf("drafted %d tasks (%d dropped) into %s\n", len(draft.Tasks), len(dropped), *out)
	if !*noBaseline {
		fmt.Printf("%d task(s) where the selector disagrees with the drafted label are listed first: each is a real confusion or a wrong label\n", mismatches)
	}
	fmt.Println("NEXT: read every task, fix or delete bad ones, set \"approved\": true on the ones you trust, then run `evolve golden approve`.")
	return nil
}

func goldenApprove(ctx context.Context) error {
	fs := flag.NewFlagSet("golden approve", flag.ContinueOnError)
	draftPath := fs.String("draft", "", "reviewed draft file (required)")
	out := fs.String("out", "", "golden set file to write (required)")
	sealedFrac := fs.Float64("sealed-frac", 0.5, "share of approved tasks held back as sealed")
	proxyCfgPath := fs.String("proxy-config", "", "optional: check labels against the live tool list")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *draftPath == "" || *out == "" {
		return fmt.Errorf("-draft and -out are required")
	}
	if _, err := os.Stat(*out); err == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite a golden set (its fingerprint is recorded in past decisions)", *out)
	}
	draft, err := selectiongate.LoadDraft(*draftPath)
	if err != nil {
		return err
	}
	var known []string
	if *proxyCfgPath != "" {
		proxyCfg, err := loadProxyConfig(*proxyCfgPath)
		if err != nil {
			return err
		}
		upstream, err := mcp.ConnectStdio(ctx, proxyCfg.Upstream)
		if err != nil {
			return err
		}
		defer upstream.Close()
		live, err := upstream.ListTools(ctx)
		if err != nil {
			return err
		}
		for _, t := range live {
			known = append(known, t.Name)
		}
	}
	g, err := draft.Approve(*sealedFrac, known)
	if err != nil {
		return err
	}
	raw, err := marshalIndent(g)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, raw, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s: %d approved tasks (%d dev, %d sealed); sealed fingerprint %s\n",
		*out, len(g.Tasks), len(g.Dev()), len(g.Sealed()), g.SealedHash())
	if len(g.Sealed()) < 30 {
		fmt.Printf("warning: only %d sealed tasks; the gate needs at least 30 (default min_sealed) to reach a verdict\n", len(g.Sealed()))
	}
	return nil
}

func marshalIndent(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }
