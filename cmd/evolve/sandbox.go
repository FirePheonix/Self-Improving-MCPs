package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"self-improving-mcp/config"
	improvementsandbox "self-improving-mcp/improvement-sandbox"
	"self-improving-mcp/mcp"
	"self-improving-mcp/store"
)

func runSandbox(ctx context.Context) error {
	fs := flag.NewFlagSet("sandbox", flag.ContinueOnError)
	cfgPath := fs.String("config", envOr("EVOLVE_CONFIG", "config/evolve.yaml"), "path to evolve yaml")
	versionID := fs.String("version", "", "candidate version id")
	proxyCfgPath := fs.String("proxy-config", envOr("PROXY_CONFIG", "config/proxy.yaml"), "proxy config for upstream")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *versionID == "" {
		return fmt.Errorf("-version is required")
	}

	cfg, err := loadEvolveConfig(*cfgPath)
	if err != nil {
		return err
	}
	db, err := openStore(cfg.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()

	ver, err := db.GetVersion(*versionID)
	if err != nil {
		return err
	}
	overlay, err := improvementsandbox.ParseOverlay(ver.OverlayJSON)
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

	calls, err := listRecentCalls(db, 50)
	if err != nil {
		return err
	}
	if len(calls) == 0 {
		return fmt.Errorf("no tool_calls to replay; use the proxy first")
	}

	baseFn := func(ctx context.Context, tool string, args map[string]any) (bool, int64, error) {
		start := time.Now()
		res, err := upstream.CallTool(ctx, tool, args)
		ms := time.Since(start).Milliseconds()
		if err != nil {
			return false, ms, err
		}
		return !res.IsError, ms, nil
	}
	candFn := improvementsandbox.WrapCall(baseFn, overlay)

	baseRes, err := improvementsandbox.ReplayTrajectory(ctx, calls, baseFn)
	if err != nil {
		return err
	}
	candRes, err := improvementsandbox.ReplayTrajectory(ctx, calls, candFn)
	if err != nil {
		return err
	}

	sc := improvementsandbox.Score(baseRes, candRes, cfg.Promote)
	exp, err := db.InsertExperiment(ver.ID, "")
	if err != nil {
		return err
	}
	status := "failed"
	if sc.Promote {
		status = "passed"
	}
	if err := db.UpdateExperimentResult(exp.ID, status, improvementsandbox.MarshalScorecard(sc)); err != nil {
		return err
	}

	raw, _ := json.MarshalIndent(sc, "", "  ")
	fmt.Println(string(raw))
	return nil
}

func loadProxyConfig(path string) (config.ProxyConfig, error) {
	cfg, err := config.LoadProxy(path)
	if err != nil {
		cfg, err = config.LoadProxy("config/proxy.example.yaml")
		if err != nil {
			return config.ProxyConfig{}, err
		}
	}
	return config.ApplyEnvProxy(cfg), nil
}

func listRecentCalls(db *store.DB, limit int) ([]store.ToolCall, error) {
	rows, err := db.SQL().Query(
		`SELECT id, session_id, seq, tool_name, args_json, args_hash,
		        result_summary, is_error, error_type, latency_ms, created_at
		 FROM tool_calls ORDER BY id DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.ToolCall
	for rows.Next() {
		var tc store.ToolCall
		var isError int
		if err := rows.Scan(
			&tc.ID, &tc.SessionID, &tc.Seq, &tc.ToolName, &tc.ArgsJSON, &tc.ArgsHash,
			&tc.ResultSummary, &isError, &tc.ErrorType, &tc.LatencyMs, &tc.CreatedAt,
		); err != nil {
			return nil, err
		}
		tc.IsError = isError != 0
		out = append(out, tc)
	}
	// replay in chronological order
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}
