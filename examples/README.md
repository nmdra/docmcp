# Client configuration

DocMCP speaks MCP over stdio, so a client launches it as a subprocess. Every file
here is the same configuration with a different name for the server entry.

| File | Client |
|---|---|
| `mcp.claude.json` | Claude Code |
| `mcp.cursor.json` | Cursor |
| `mcp.pi.json` | Pi |

## Install

The `command` above is the bare binary name, which requires `docmcp` on your
`PATH`. If you built it somewhere else, give the absolute path instead:

```json
{
  "mcpServers": {
    "docmcp": {
      "command": "/usr/local/bin/docmcp",
      "args": ["serve"]
    }
  }
}
```

## Before it can answer anything

The server reads an index. Index at least one site first:

```bash
docmcp add https://example.com/docs/ --name example
```

Then restart the MCP client so it picks up the server.

## Non-default data location

If your index lives somewhere other than the default data directory, pass it
explicitly:

```json
{
  "mcpServers": {
    "docmcp": {
      "command": "docmcp",
      "args": ["serve", "--data-dir", "/path/to/data"]
    }
  }
}
```
