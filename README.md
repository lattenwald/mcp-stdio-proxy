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
- **Smart auto-discovery**: Automatically finds and prioritizes project-local mcp-hub instances
- **Fast**: Go-based, low latency, minimal memory footprint

## Installation

```bash
cd ~/projects/go/mcp-stdio-proxy
go build -o mcp-stdio-proxy
```

## Usage

```bash
# Basic usage with explicit URL
./mcp-stdio-proxy http://localhost:37373/mcp

# Auto-discover local mcp-hub port (recommended!)
./mcp-stdio-proxy --mcp-hub

# With custom timeout (default: 120 seconds)
./mcp-stdio-proxy --timeout 300 http://localhost:37373/mcp

# With debug logging
./mcp-stdio-proxy --debug http://localhost:37373/mcp
./mcp-stdio-proxy --mcp-hub --debug

# Get help
./mcp-stdio-proxy --help

# Use with Claude Code in .claude.json
{
  "mcpServers": {
    "hub": {
      "command": "/path/to/mcp-stdio-proxy",
      "args": ["--mcp-hub"]
    }
  }
}

# Or with explicit URL
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
      "args": ["--mcp-hub", "--debug"]
    }
  }
}
```

### Options

- `--mcp-hub` - Auto-discover local mcp-hub port (no URL needed!)
- `--timeout` - HTTP request timeout in seconds (default: 120)
- `--debug` / `-v` / `--verbose` - Enable debug logging to stderr
- `--help` / `-h` - Show help message

### Port Auto-Discovery

The `--mcp-hub` flag automatically finds mcp-hub running on your local machine:

1. **Process list search**: Scans for `mcp-hub` process and extracts `--port` argument
2. **Smart prioritization**: When multiple mcp-hub instances are found, prioritizes project-local configurations
3. **Network socket fallback**: Uses `ss` or `netstat` to find listening port

This eliminates the need to manually track which port mcp-hub is running on, especially useful when mcp-hub dynamically selects ports.

#### Smart Instance Selection

When multiple mcp-hub instances are running, the proxy intelligently selects the most relevant one:

**Prioritization Logic:**
- ✅ **Prefers project-local configs** over global `~/.mcp-hub/` configs
- ✅ **Scores by proximity** to current working directory
- ✅ **Parent directory bonus** - favors configs in parent directories (typical project structure)
- ✅ **Shows scoring details** with `--debug` flag

**Example Scenario:**
```bash
# Two mcp-hub instances running:
# 1. Port 37373 - global config: ~/.mcp-hub/config.json
# 2. Port 40808 - project config: ~/myproject/.mcphub/servers.json

# When running from ~/myproject/src/
./mcp-stdio-proxy --mcp-hub --debug

# Output (debug mode):
# [DISCOVERY] Instance scoring:
#   Instance 1 (port 40808): score=500 - project-local config
#   Instance 2 (port 37373): score=0 - global config only
# [DISCOVERY] Selected instance with port 40808
```

This allows seamless switching between projects - the proxy automatically connects to the project-specific mcp-hub instance based on your current directory.

#### Process Visibility

When using `--mcp-hub` mode, the proxy re-executes itself with enriched arguments to make connection details visible in `ps` output:

```bash
# User runs:
./mcp-stdio-proxy --mcp-hub

# After auto-discovery, appears in ps as:
./mcp-stdio-proxy --mcp-hub-config /path/to/.mcphub/servers.json http://localhost:40808/mcp
```

**Benefits:**
- ✅ **Easily identify** which mcp-hub instance each proxy is connected to
- ✅ **See config path** to understand which project's config is being used
- ✅ **Debug multi-instance setups** with `ps aux | grep mcp-stdio-proxy`

**Example `ps` output:**
```bash
$ ps aux | grep mcp-stdio-proxy
user  12345  ./mcp-stdio-proxy --mcp-hub-config ~/.mcp-hub/config.json http://localhost:37373/mcp
user  12346  ./mcp-stdio-proxy --mcp-hub-config ~/project/.mcphub/servers.json http://localhost:40808/mcp
user  12347  ./mcp-stdio-proxy http://localhost:9999/mcp
```

Debug logging can also be enabled via environment variable:
```bash
DEBUG=1 ./mcp-stdio-proxy http://localhost:37373/mcp
DEBUG=1 ./mcp-stdio-proxy --mcp-hub
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

### mcp-hub Compatibility

⚠️ **mcp-hub v4.2.1 (Current Release)**
- Currently **not supported** (uses non-standard transport)
- mcp-hub uses custom two-endpoint SSE pattern (GET /mcp + POST /messages)
- See [docs/MCP-HUB-QUIRKS.md](docs/MCP-HUB-QUIRKS.md) for technical details

🎉 **mcp-hub (Upcoming - After PR #128 Merge)**
- **Will be fully supported** with NO proxy changes needed!
- [PR #128](https://github.com/ravitemer/mcp-hub/pull/128) adds standard Streamable HTTP support
- Backward compatible with legacy SSE transport
- Upgrade mcp-hub when released: `npm install -g @ravitemer/mcp-hub@latest`

#### Using the Proxy with mcp-hub PR #128 (Testing)

If you want to test before the official release:

```bash
# Clone and checkout PR branch
cd ~/git/mcp-hub
git fetch origin pull/128/head:pr-128
git checkout pr-128
npm install
npm start

# Then use proxy as normal (no special flags needed!)
./mcp-stdio-proxy http://localhost:37373/mcp
```

#### Timeline

- **Now**: Works with MCP spec-compliant servers
- **Waiting**: PR #128 merge (adds mcp-hub compatibility)
- **After merge**: Works with mcp-hub out of the box
- **Phase 4 cancelled**: `--mcp-hub-mode` flag no longer needed (saves 4-6 hours development)

## Documentation

- [PRD](docs/PRD.md) - Product Requirements Document (includes Phase 4 plan)
- [PROGRESS](docs/PROGRESS.md) - Implementation progress and decisions
- [MCP-HUB-QUIRKS](docs/MCP-HUB-QUIRKS.md) - mcp-hub compatibility analysis
- [MCP Specification](https://modelcontextprotocol.io/specification/2025-03-26/basic/transports)

## License

MIT
