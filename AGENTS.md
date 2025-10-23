# Agent Context: mcp-stdio-proxy

> This file provides context for AI assistants (Claude, Gemini, Grok, etc.) collaborating on this project.

## Project Overview

**mcp-stdio-proxy** is a minimal Go-based proxy that bridges stdio transport to Streamable HTTP for the Model Context Protocol (MCP).

### Problem Being Solved
Existing Python mcp-proxy (v0.9.0) has protocol incompatibilities with mcp-hub (v4.2.1), causing intermittent OAuth failures. Need a clean, minimal implementation that works reliably.

### Architecture
```
Claude Code (stdio) → mcp-stdio-proxy → mcp-hub (Streamable HTTP) → Backend MCP Servers
                                                                      (Atlassian, GitLab, Zen)
```

## Project Status

**Current Phase**: Testing and validation

### Completed
- [x] Project structure created
- [x] Go module initialized
- [x] README.md written
- [x] docs/PRD.md completed
- [x] AGENTS.md created
- [x] docs/PROGRESS.md created for tracking
- [x] main.go implemented (~666 lines)
- [x] CLI argument parsing and validation
- [x] Proxy struct and initialization
- [x] Stdin message reader (newline-delimited JSON-RPC)
- [x] HTTP request builder with proper headers
- [x] Session ID management (Mcp-Session-Id header)
- [x] JSON and SSE response handlers
- [x] HTTP error to JSON-RPC conversion
- [x] Exponential backoff retry logic (3 attempts)
- [x] Debug logging support (--debug, -v, --verbose flags + DEBUG=1)
- [x] **mcp-hub port auto-discovery** (--mcp-hub flag)
  - Process list scanning with --port extraction
  - Network socket fallback (ss/netstat)
  - **Smart instance selection** - prioritizes project-local configs over global configs
  - Proximity scoring based on current working directory
- [x] Binary built successfully (8.4 MB)
- [x] Updated README with usage examples and options
- [x] Updated PROGRESS.md with technical decisions

### Next Steps
- [x] Test with local mcp-hub instance (discovered protocol incompatibility)
- [x] Validate initialization sequence (works with standard Streamable HTTP)
- [x] Verify session ID persistence (works with standard Streamable HTTP)
- [x] Test error handling scenarios (all passing)
- [x] Analyze mcp-hub source code
- [x] Discovered PR #128 adds standard Streamable HTTP to mcp-hub
- [ ] ~~Implement Phase 4: mcp-hub mode support~~ **CANCELLED** (PR #128 makes it obsolete)
- [ ] Monitor PR #128 merge status
- [ ] Test with mcp-hub after PR #128 merge
- [ ] Update documentation after official release

## Technical Decisions

### Language: Go
**Rationale**:
- Fast development (2-4 hours vs 4-6 for Rust)
- Excellent stdlib HTTP/SSE support
- Simple concurrency model
- User comfortable with Go

### Scope: Minimal
**Philosophy**: Do one thing well
- No configuration files (URL as CLI arg or auto-discovered)
- No multi-server aggregation (mcp-hub handles this)
- No OAuth (downstream server's responsibility)
- ~666 lines of code (includes smart auto-discovery with project-local prioritization)

### Protocol: MCP 2025-03-26 Streamable HTTP
**Key Requirements**:
- Mcp-Session-Id header management
- SSE response parsing
- Newline-delimited JSON-RPC on stdio
- HTTP POST for all messages

## Code Guidelines

### Go Idioms
- Use `bufio.Scanner` for stdin reading
- Use `net/http` client with connection pooling
- Handle errors explicitly, no panic except in main
- Log to stderr only (stdout reserved for JSON-RPC)

### Error Handling
- Connection errors: Retry with exponential backoff
- Malformed messages: Skip, log to stderr
- HTTP errors: Convert to JSON-RPC error responses
- Always preserve JSON-RPC message ID for responses

### Testing Strategy
- Manual testing with mcp-hub on localhost:37373
- Test cases: initialization, multi-message session, errors
- Validate session ID persistence

## Environment Context

### User Setup
- **OS**: Linux (6.17.0-5-generic)
- **Go**: 1.24.4 (managed via asdf)
- **mcp-hub**: v4.2.1 running on localhost:37373
- **Backend servers**: Atlassian, GitLab, Zen (all connected)

### Development Workflow
- Project location: `~/projects/go/mcp-stdio-proxy/`
- Build: `go build -o mcp-stdio-proxy`
- Test: `echo '{"jsonrpc":"2.0",...}' | ./mcp-stdio-proxy http://localhost:37373/mcp`

## Collaboration Notes

### For Claude
- Focus on clean, maintainable code
- Reference PRD.md for detailed requirements
- Ask before adding complexity beyond PRD scope

### For Gemini
- Prioritize Go best practices and idioms
- Consider performance but don't over-optimize
- Validate protocol compliance with MCP spec

### For Grok
- Architectural review and edge case analysis
- Performance profiling if needed
- Security considerations

## Key Files

- **README.md**: User-facing documentation
- **docs/PRD.md**: Complete requirements and implementation plan (includes Phase 4)
- **docs/PROGRESS.md**: Implementation progress tracking
- **docs/MCP-HUB-QUIRKS.md**: mcp-hub protocol analysis and compatibility plan
- **main.go**: Core proxy implementation (~480 lines, includes port auto-discovery)
- **go.mod**: Go module definition
- **mcp-stdio-proxy**: Compiled binary (~8.5 MB)

## References

- [MCP 2025-03-26 Spec](https://modelcontextprotocol.io/specification/2025-03-26/basic/transports)
- [mcp-hub Repository](https://github.com/ravitemer/mcp-hub)
- PRD.md for detailed technical requirements

## Communication Protocol

When working on this project:
1. Check AGENTS.md for current status
2. Update status sections after completing work
3. Document decisions and rationale
4. Cross-reference PRD.md for requirements
5. Keep README.md user-focused, AGENTS.md AI-focused

## Git Workflow Guidelines

### Commit Policy
**⚠️ IMPORTANT: Only commit when explicitly requested by the user**

- **NEVER** run `git add` or `git commit` automatically
- **ALWAYS** ask user before committing changes
- **ALWAYS** show what will be committed (`git status`) before asking
- User must explicitly request: "commit this" or "commit these changes"

### Commit Message Format

**Format**: Concise, high-level commits
- **One line** if possible
- **Bullet list** with bird's-eye view points if needed
- **No details** - those belong in code and documentation
- Focus on **what changed**, not **how** or **why**

**Good Examples**:
```
Add mcp-stdio-proxy with debug flags and mcp-hub analysis
```

```
Implement stdio to HTTP proxy
- Add CLI flags for debug logging
- Document mcp-hub compatibility issues
```

**Bad Examples**:
```
Add mcp-stdio-proxy implementation with mcp-hub analysis

## Implementation Complete (Phase 1-3)

Core Features:
- stdio to Streamable HTTP proxy (MCP 2025-03-26 spec)
... [detailed feature list]
```
(Too detailed - this belongs in documentation)

### Pre-Commit Checklist

Before asking user to commit:
1. Run `git status` to see what changed
2. Ensure all changes are intentional
3. Verify no sensitive data or temporary files
4. Present concise commit message for approval
5. Wait for explicit user confirmation

---

**Last Updated**: 2025-10-10
**Current Focus**: Monitoring mcp-hub PR #128 for standard protocol support

## mcp-hub Compatibility Analysis (2025-10-10)

### UPDATE: PR #128 Solves Compatibility Issue! 🎉

**Discovery**: Pull request [#128](https://github.com/ravitemer/mcp-hub/pull/128) adds full MCP 2025-03-26 Streamable HTTP support to mcp-hub!

**Impact**:
- ✅ Our proxy will work with mcp-hub **with NO code changes**
- ✅ Phase 4 implementation **CANCELLED** (saves 4-6 hours)
- ✅ Standard protocol compliance validated
- ⏳ Waiting on PR merge to production release

**See**: [docs/MCP-HUB-QUIRKS.md](docs/MCP-HUB-QUIRKS.md) UPDATE section for full analysis

## mcp-hub Compatibility Analysis (Original Discovery - 2025-10-10)

### Discovery
Tested proxy with mcp-hub v4.2.1 and discovered it **does not implement standard MCP Streamable HTTP**. Instead uses custom two-endpoint SSE pattern.

### Protocol Differences
| Aspect | MCP Spec | mcp-hub v4.2.1 |
|--------|----------|----------------|
| Endpoints | Single POST | GET /mcp + POST /messages |
| Session ID | HTTP header | Query parameter |
| Responses | POST response (JSON/SSE) | Always SSE stream |
| Transport | Unified | Split (GET for responses, POST for requests) |

### Documentation
- **Technical Analysis**: `docs/MCP-HUB-QUIRKS.md`
- **Implementation Plan**: `docs/PRD.md` Phase 4
- **Source Analysis**: Examined `~/git/mcp-hub/src/`

### Status
- ✅ Current proxy: Works with MCP spec-compliant servers
- ⏳ mcp-hub support: Planned for Phase 4 implementation
- 📋 Estimated effort: 4-6 hours for mcp-hub mode

## Implementation Summary

### Code Structure
**main.go** contains:
- `Proxy` struct: Core proxy state (URL, session ID, HTTP client, I/O)
- `JSONRPCMessage` struct: JSON-RPC 2.0 message representation
- `McpHubInstance` struct: Discovered mcp-hub process details
- `Run()`: Main event loop reading from stdin
- `forwardMessage()`: Retry logic wrapper
- `sendHTTPRequest()`: HTTP POST with session management
- `handleJSONResponse()`: Parse and write JSON responses
- `handleSSEResponse()`: Parse SSE streams
- `writeSSEData()`: Validate and write SSE data to stdout
- `sendErrorResponse()`: Convert errors to JSON-RPC format
- `discoverMcpHubPort()`: Auto-discover mcp-hub port with smart selection
- `findAllMcpHubInstances()`: Scan all mcp-hub processes with details
- `selectBestMcpHubInstance()`: Smart instance selection with scoring
- `scoreInstance()`: Calculate priority score based on config proximity
- `commonPathLength()`: Calculate path similarity metric
- `findPortInNetstat()`: Find port from network sockets
- `tryNetworkCommand()`: Helper for ss/netstat parsing

### Key Features Implemented
1. **Protocol Compliance**: Full MCP 2025-03-26 Streamable HTTP support
2. **Session Persistence**: Automatic Mcp-Session-Id header management
3. **Dual Response Handling**: Both JSON and SSE (text/event-stream) responses
4. **Resilience**: 3-attempt exponential backoff (100ms, 200ms, 400ms)
5. **Debug Mode**: Multiple options (--debug, -v, --verbose, DEBUG=1)
6. **Error Handling**: Malformed messages skipped, HTTP errors converted to JSON-RPC
7. **CLI Interface**: Standard flag package, comprehensive help message
8. **Smart Auto-Discovery**: --mcp-hub flag with intelligent instance selection
   - Finds all running mcp-hub instances
   - Prioritizes project-local configs over global configs
   - Scores by proximity to current working directory
   - Shows detailed scoring in debug mode
9. **Zero Dependencies**: Pure Go stdlib implementation

### CLI Usage
```bash
# Auto-discover mcp-hub port (recommended)
./mcp-stdio-proxy --mcp-hub

# Explicit URL
./mcp-stdio-proxy http://localhost:37373/mcp

# With debug logging
./mcp-stdio-proxy --mcp-hub --debug
./mcp-stdio-proxy --debug http://localhost:37373/mcp

# Get help
./mcp-stdio-proxy --help
```

### Testing Ready
Binary location: `./mcp-stdio-proxy`
Test commands:
- Auto-discovery: `echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | ./mcp-stdio-proxy --mcp-hub`
- Explicit URL: `echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | ./mcp-stdio-proxy http://localhost:37373/mcp`
