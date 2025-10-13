# Product Requirements Document: mcp-stdio-proxy

## Problem Statement

Current MCP proxy implementations (Python mcp-proxy v0.9.0) have protocol incompatibilities with modern Streamable HTTP servers like mcp-hub v4.2.1, causing intermittent OAuth authentication failures and generic error messages.

## Solution

Build a minimal, focused stdio ↔ Streamable HTTP proxy that implements the MCP 2025-03-26 specification correctly.

## Goals

### Primary Goals
1. **Reliable protocol bridging**: stdio (newline-delimited JSON-RPC) ↔ Streamable HTTP
2. **Session management**: Proper Mcp-Session-Id header handling
3. **Immediate usability**: Works today with existing mcp-hub setup
4. **Minimal complexity**: Single file, ~200-300 lines of Go

### Non-Goals
- Multi-server aggregation (use mcp-hub for this)
- Configuration files (URL passed as argument)
- OAuth/authentication (handled by downstream server)
- Transport protocol detection (focus on Streamable HTTP only)

## Technical Requirements

### Input (stdio)
- Read newline-delimited JSON-RPC messages from stdin
- UTF-8 encoding
- No embedded newlines in messages
- Standard MCP message format

### Output (Streamable HTTP)
- HTTP POST to configured endpoint
- Content-Type: application/json
- Accept: application/json, text/event-stream
- Include Mcp-Session-Id header on subsequent requests

### Response Handling
- Parse Mcp-Session-Id from initial response
- Store session ID for connection lifetime
- Stream SSE response back to stdout as JSON-RPC messages
- Handle both immediate JSON responses and SSE streams

### Error Handling
- Connection failures: Retry with exponential backoff
- Invalid messages: Log to stderr, continue processing
- JSON-RPC errors: Pass through to stdout
- HTTP errors: Convert to JSON-RPC error responses

## Architecture

### Components

```
┌─────────────┐         ┌──────────────────┐         ┌─────────────┐
│ Claude Code │ stdio   │ mcp-stdio-proxy  │  HTTP   │   mcp-hub   │
│   (client)  │────────▶│                  │────────▶│   (server)  │
│             │◀────────│  - stdin reader  │◀────────│             │
└─────────────┘         │  - HTTP client   │         └─────────────┘
                        │  - SSE parser    │
                        │  - stdout writer │
                        └──────────────────┘
```

### Message Flow

1. **Initialization**
   ```
   Client → stdin: {"jsonrpc":"2.0","id":1,"method":"initialize",...}
   Proxy → HTTP POST /mcp
   Server → Response with Mcp-Session-Id header
   Proxy → stdout: {"jsonrpc":"2.0","id":1,"result":{...}}
   Proxy stores session ID
   ```

2. **Subsequent Messages**
   ```
   Client → stdin: {"jsonrpc":"2.0","id":2,"method":"tools/list"}
   Proxy → HTTP POST /mcp (with Mcp-Session-Id header)
   Server → SSE stream or JSON response
   Proxy → stdout: {"jsonrpc":"2.0","id":2,"result":{...}}
   ```

### Key Data Structures

```go
type Proxy struct {
    url       string
    sessionID string
    client    *http.Client
    stdin     *bufio.Scanner  // 1MB buffer (increased from 64KB default)
    stdout    io.Writer
    debug     bool
}

type JSONRPCMessage struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      json.RawMessage `json:"id,omitempty"`
    Method  string          `json:"method,omitempty"`
    Params  json.RawMessage `json:"params,omitempty"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *JSONRPCError   `json:"error,omitempty"`
}
```

## Implementation Plan

### Phase 1: Core Proxy (COMPLETED)
- [x] CLI argument parsing (URL)
- [x] stdin reader (newline-delimited)
- [x] HTTP POST client
- [x] Basic response handling
- [x] stdout writer

### Phase 2: Protocol Compliance (COMPLETED)
- [x] Session ID extraction and storage
- [x] Mcp-Session-Id header injection
- [x] SSE parsing and streaming
- [x] Error conversion to JSON-RPC format
- [x] Debug logging (--debug, -v, --verbose flags)
- [x] Retry logic with exponential backoff

### Phase 3: Testing (COMPLETED)
- [x] Test with mcp-hub PR #128
- [x] Test initialization sequence
- [x] Test multi-message sessions
- [x] Test error conditions
- [x] Test large responses (tools/list with 69 tools)

### Phase 4: Buffer Size Fix (COMPLETED)
- [x] Increase scanner buffers from 64KB to 1MB
- [x] Handle large tool lists without buffer overflow

## Success Criteria

1. ✓ Claude Code connects successfully through proxy to mcp-hub
2. ✓ No "Error" messages during normal operation
3. ✓ Session persistence across multiple messages
4. ✓ All backend MCP tools accessible
5. ✓ Response latency < 50ms added overhead

## Edge Cases

1. **Connection loss**: ✅ Retry with exponential backoff (3 attempts: 100ms, 200ms, 400ms)
2. **Large responses**: ✅ Stream SSE data without buffering entirely (1MB buffer handles 69-tool lists)
3. **Concurrent messages**: ✅ Sequential processing (one message at a time via stdin)
4. **Malformed input**: ✅ Skip invalid messages, log to stderr
5. **Server errors**: ✅ Properly format HTTP 4xx/5xx as JSON-RPC errors

## Monitoring & Debugging

✅ **Implemented**:
- CLI flags: `--debug`, `-v`, `--verbose`
- Environment variable: `DEBUG=1`
- Log format: `[TAG] message` (e.g., `[INIT]`, `[HTTP]`, `[SSE]`, `[SESSION]`)
- All logs to stderr only (stdout reserved for JSON-RPC messages)
- Session ID tracking in debug output

## mcp-hub Compatibility

### Discovery (2025-10-10)

Analysis of mcp-hub v4.2.1 revealed it **does not implement standard MCP Streamable HTTP**. Instead, it uses a custom two-endpoint SSE pattern:

1. `GET /mcp` - Establishes SSE connection, returns sessionId
2. `POST /messages?sessionId=xxx` - Sends messages
3. Responses arrive via SSE stream from step 1

**See**: [docs/MCP-HUB-QUIRKS.md](MCP-HUB-QUIRKS.md) for detailed analysis.

### UPDATE: PR #128 Solves Compatibility Issue! 🎉

**Discovery**: Pull request [#128](https://github.com/ravitemer/mcp-hub/pull/128) adds full MCP 2025-03-26 Streamable HTTP support to mcp-hub!

**Impact**:
- ✅ Our proxy works with mcp-hub **with NO code changes**
- ✅ POST /mcp endpoint with standard protocol
- ✅ Mcp-Session-Id header management
- ✅ Tested successfully with 69 tools
- ⏳ Waiting on PR merge to production release

**See**: [docs/PR128-ANALYSIS.md](PR128-ANALYSIS.md) for complete analysis and [docs/MCP-HUB-QUIRKS.md](MCP-HUB-QUIRKS.md) for installation instructions.

### ~~Phase 4: mcp-hub Mode Support~~ **CANCELLED**

~~Add `--mcp-hub-mode` flag to support mcp-hub's split-endpoint transport~~

**Status**: **Not needed** - PR #128 makes mcp-hub spec-compliant, eliminating the need for a compatibility mode.

**Time Saved**: 4-6 hours of development + ongoing maintenance

**Recommendation**: Wait for PR #128 merge, then test with official release. The proxy already implements everything needed.

## Future Enhancements (Out of Scope)

- Multiple server support
- Configuration file
- OAuth token handling
- Metrics/telemetry
- WebSocket transport
- TLS client certificates
- Auto-detection of mcp-hub vs standard servers
