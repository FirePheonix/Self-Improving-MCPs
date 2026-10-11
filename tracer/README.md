# tracer

Records every tool call into trajectories.

Writes session id, sequence number, tool name, redacted args, result summary, latency, and errors into the shared store. No model chain-of-thought.

## Inspect trajectories (SQLite)

Default DB: `data/trajectories.db`

```powershell
sqlite3 data/trajectories.db
```

```sql
-- recent sessions
SELECT id, started_at, upstream FROM sessions ORDER BY started_at DESC LIMIT 10;

-- calls in a session (replace SESSION_ID)
SELECT seq, tool_name, latency_ms, is_error, result_summary
FROM tool_calls
WHERE session_id = 'SESSION_ID'
ORDER BY seq;

-- hottest tools
SELECT tool_name, COUNT(*) AS n, AVG(latency_ms) AS avg_ms
FROM tool_calls
GROUP BY tool_name
ORDER BY n DESC;
```
