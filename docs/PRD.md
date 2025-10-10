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
    stdin     *bufio.Scanner
    stdout    io.Writer
}
```

## Implementation Plan

### Phase 1: Core Proxy (2-3 hours)
- [ ] CLI argument parsing (URL)
- [ ] stdin reader (newline-delimited)
- [ ] HTTP POST client
- [ ] Basic response handling
- [ ] stdout writer

### Phase 2: Protocol Compliance (1-2 hours)
- [ ] Session ID extraction and storage
- [ ] Mcp-Session-Id header injection
- [ ] SSE parsing and streaming
- [ ] Error conversion to JSON-RPC format

### Phase 3: Testing (1 hour)
- [ ] Test with mcp-hub
- [ ] Test initialization sequence
- [ ] Test multi-message sessions
- [ ] Test error conditions

## Success Criteria

1. ✓ Claude Code connects successfully through proxy to mcp-hub
2. ✓ No "Error" messages during normal operation
3. ✓ Session persistence across multiple messages
4. ✓ All backend MCP tools accessible
5. ✓ Response latency < 50ms added overhead

## Edge Cases

1. **Connection loss**: Implement reconnection with session recovery
2. **Large responses**: Stream SSE data without buffering entirely
3. **Concurrent messages**: Handle request/response ordering (JSON-RPC ID matching)
4. **Malformed input**: Skip invalid messages, log to stderr
5. **Server errors**: Properly format HTTP 4xx/5xx as JSON-RPC errors

## Monitoring & Debugging

- Environment variable `DEBUG=1` enables verbose logging to stderr
- Log format: timestamp, message type, direction, session ID
- No logging to stdout (reserved for JSON-RPC messages)

## mcp-hub Compatibility

### Discovery (2025-10-10)

Analysis of mcp-hub v4.2.1 revealed it **does not implement standard MCP Streamable HTTP**. Instead, it uses a custom two-endpoint SSE pattern:

1. `GET /mcp` - Establishes SSE connection, returns sessionId
2. `POST /messages?sessionId=xxx` - Sends messages
3. Responses arrive via SSE stream from step 1

**See**: [docs/MCP-HUB-QUIRKS.md](MCP-HUB-QUIRKS.md) for detailed analysis.

### Phase 4: mcp-hub Mode Support (4-6 hours)

Add `--mcp-hub-mode` flag to support mcp-hub's split-endpoint transport:

**Architecture Changes**:
```go
type Proxy struct {
    url        string
    sessionID  string
    client     *http.Client
    stdin      *bufio.Scanner
    stdout     io.Writer
    debug      bool
    mcpHubMode bool              // NEW: Enable mcp-hub compatibility
    sseConn    *http.Response    // NEW: Long-lived SSE connection
    sseReader  *bufio.Scanner    // NEW: SSE stream reader
    pendingReqs map[string]chan *JSONRPCMessage  // NEW: Request/response correlation
}
```

**Implementation Tasks**:
- [ ] Add `--mcp-hub-mode` CLI flag
- [ ] Implement GET /mcp SSE connection establishment
- [ ] Parse sessionId from SSE transport (may need custom parsing)
- [ ] Background goroutine for SSE stream reading
- [ ] Request/response correlation via JSON-RPC ID matching
- [ ] POST to /messages?sessionId=xxx for each stdin message
- [ ] Channel-based async response handling
- [ ] Reconnection logic for SSE connection loss
- [ ] Update error handling for mcp-hub mode
- [ ] Add mcp-hub mode tests

**Success Criteria**:
1. ✓ Proxy connects to mcp-hub with `--mcp-hub-mode`
2. ✓ Initialization succeeds and tools/list works
3. ✓ Multi-message sessions work correctly
4. ✓ Async responses properly correlated
5. ✓ Standard mode unchanged (backward compatible)

**Technical Challenges**:
- Async response handling (responses don't match POST order)
- SSE connection keepalive and reconnection
- sessionId extraction (may require parsing SSE handshake)
- Concurrent request handling with proper correlation

## Future Enhancements (Out of Scope)

- Multiple server support
- Configuration file
- OAuth token handling
- Metrics/telemetry
- WebSocket transport
- TLS client certificates
- Auto-detection of mcp-hub vs standard servers
