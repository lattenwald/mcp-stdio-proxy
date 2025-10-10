# mcp-stdio-proxy

A minimal stdio to Streamable HTTP proxy for Model Context Protocol (MCP).

## Purpose

This proxy bridges the gap between stdio-based MCP clients (like Claude Code) and Streamable HTTP MCP servers (like mcp-hub). It handles the protocol translation transparently.

## Architecture

```
Claude Code (stdio) → mcp-stdio-proxy → mcp-hub (Streamable HTTP) → Backend MCP Servers
```

## Features

- **Minimal**: Single binary, no configuration files required
- **Protocol compliant**: Implements MCP 2025-03-26 Streamable HTTP specification
- **Session management**: Handles Mcp-Session-Id headers automatically
- **Fast**: Go-based, low latency, minimal memory footprint

## Installation

```bash
cd ~/projects/go/mcp-stdio-proxy
go build -o mcp-stdio-proxy
```

## Usage

```bash
# Basic usage
./mcp-stdio-proxy http://localhost:37373/mcp

# With debug logging
./mcp-stdio-proxy --debug http://localhost:37373/mcp
./mcp-stdio-proxy -v http://localhost:37373/mcp

# Get help
./mcp-stdio-proxy --help

# Use with Claude Code in .claude.json
{
  "mcpServers": {
    "hub": {
      "command": "/path/to/mcp-stdio-proxy",
      "args": ["http://localhost:37373/mcp"]
    }
  }
}

# Enable debug logging in Claude Code
{
  "mcpServers": {
    "hub": {
      "command": "/path/to/mcp-stdio-proxy",
      "args": ["--debug", "http://localhost:37373/mcp"]
    }
  }
}
```

### Options

- `--debug` / `-v` / `--verbose` - Enable debug logging to stderr
- `--help` / `-h` - Show help message

Debug logging can also be enabled via environment variable:
```bash
DEBUG=1 ./mcp-stdio-proxy http://localhost:37373/mcp
```

## Requirements

- Go 1.21 or later
- Running MCP server with Streamable HTTP transport

## Compatibility

### Supported Servers

✅ **MCP Spec-Compliant Servers**
- Any server implementing [MCP 2025-03-26 Streamable HTTP](https://modelcontextprotocol.io/specification/2025-03-26/basic/transports)
- Single POST endpoint with SSE or JSON responses
- Session management via `Mcp-Session-Id` header

⚠️ **mcp-hub v4.2.1**
- Currently **not supported** (uses non-standard transport)
- mcp-hub uses custom two-endpoint SSE pattern (GET /mcp + POST /messages)
- Support planned in Phase 4 via `--mcp-hub-mode` flag
- See [docs/MCP-HUB-QUIRKS.md](docs/MCP-HUB-QUIRKS.md) for technical details

### Future Compatibility

Phase 4 will add mcp-hub support:
```bash
# For mcp-hub v4.2.1+
./mcp-stdio-proxy --mcp-hub-mode http://localhost:37373
```

## Documentation

- [PRD](docs/PRD.md) - Product Requirements Document (includes Phase 4 plan)
- [PROGRESS](docs/PROGRESS.md) - Implementation progress and decisions
- [MCP-HUB-QUIRKS](docs/MCP-HUB-QUIRKS.md) - mcp-hub compatibility analysis
- [MCP Specification](https://modelcontextprotocol.io/specification/2025-03-26/basic/transports)

## License

MIT
