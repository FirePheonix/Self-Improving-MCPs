# mcp

Cursor-facing MCP proxy.

Talks to Cursor over stdio as an MCP server, and to your real upstream MCP as a client.
Lists and forwards tools 1:1. Later applies version overlays (better descriptions, routing, cache, composed tools) without changing upstream source.
