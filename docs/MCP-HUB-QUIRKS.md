# mcp-hub Transport Protocol Analysis

**Date**: 2025-10-10
**mcp-hub Version**: 4.2.1
**MCP Spec**: 2025-03-26

## Executive Summary

mcp-hub v4.2.1 **does not implement the standard MCP Streamable HTTP transport** as defined in the MCP specification. Instead, it uses a **custom two-endpoint SSE pattern** that requires different proxy handling.

## Standard MCP Streamable HTTP Transport

According to the [MCP 2025-03-26 specification](https://modelcontextprotocol.io/specification/2025-03-26/basic/transports#streamable-http-with-server-sent-events-sse), the Streamable HTTP transport should work as follows:

### Expected Behavior (MCP Spec)
```
Client → POST /endpoint
  Headers:
    - Content-Type: application/json
    - Accept: application/json, text/event-stream
    - Mcp-Session-Id: <session-id> (on subsequent requests)
  Body: JSON-RPC message

Server → Response
  - First request: Return Mcp-Session-Id header
  - Response: Either JSON (Content-Type: application/json) or SSE stream (text/event-stream)
```

### Key Characteristics
- **Single endpoint**: All communication via POST to one URL
- **Session management**: Via `Mcp-Session-Id` header
- **Response flexibility**: Server chooses JSON or SSE per-request
- **Bidirectional**: Both requests and responses in one connection

## mcp-hub's Actual Implementation

### Architecture
mcp-hub implements a **split-endpoint SSE transport**:

```javascript
// From src/server.js

// Endpoint 1: SSE Connection (GET)
app.get("/mcp", async (req, res) => {
  await mcpServerEndpoint.handleSSEConnection(req, res);
});

// Endpoint 2: Message Handling (POST)
app.post("/messages", async (req, res) => {
  await mcpServerEndpoint.handleMCPMessage(req, res);
});
```

### Protocol Flow

**Step 1: Establish SSE Connection**
```http
GET /mcp HTTP/1.1
Host: localhost:37373

Response:
  Content-Type: text/event-stream
  - Creates SSEServerTransport
  - Generates unique sessionId
  - Keeps connection open for server→client messages
```

**Step 2: Send Messages**
```http
POST /messages?sessionId=<session-id> HTTP/1.1
Content-Type: application/json

{"jsonrpc":"2.0","id":1,"method":"initialize",...}

Response:
  Status: 200 OK (acknowledgment only)
```

**Step 3: Receive Responses**
```
← SSE stream from GET /mcp connection
data: {"jsonrpc":"2.0","id":1,"result":{...}}
```

### Key Differences from MCP Spec

| Aspect | MCP Spec | mcp-hub Implementation |
|--------|----------|----------------------|
| **Endpoints** | Single POST endpoint | Two endpoints (GET + POST) |
| **Connection type** | POST with optional SSE | GET for SSE + POST for messages |
| **Session ID** | HTTP header | Query parameter |
| **Response channel** | POST response (JSON/SSE) | Always via SSE stream from GET |
| **Bidirectional** | Yes (single connection) | Split (GET for responses, POST for requests) |

## Code Analysis

### SSE Connection Setup
**File**: `src/mcp/server.js:475-513`

```javascript
async handleSSEConnection(req, res) {
  // Create SSE transport with /messages endpoint
  const transport = new SSEServerTransport('/messages', res);
  const sessionId = transport.sessionId;

  // Create new server instance per connection
  const server = this.createServer();
  this.clients.set(sessionId, { transport, server });

  // Setup cleanup on disconnect
  res.on("close", cleanup);

  // Connect server to transport
  await server.connect(transport);
}
```

**Key observations**:
- Uses `@modelcontextprotocol/sdk` SSEServerTransport
- Each client gets a unique server instance
- Transport manages sessionId internally
- Cleanup on connection close

### Message Handling
**File**: `src/mcp/server.js:518-543`

```javascript
async handleMCPMessage(req, res) {
  const sessionId = req.query.sessionId;

  if (!sessionId) {
    return sendErrorResponse(400, new Error('Missing sessionId'));
  }

  const transportInfo = this.clients.get(sessionId);
  if (transportInfo) {
    await transportInfo.transport.handlePostMessage(req, res, req.body);
  } else {
    return sendErrorResponse(404, new Error('Session not found'));
  }
}
```

**Key observations**:
- Session ID passed as query parameter
- Routes message to existing transport
- Returns 404 if session not found
- Acknowledgment response only (actual data via SSE)

## Why This Matters for mcp-stdio-proxy

### Current Proxy Implementation
Our proxy currently implements **standard MCP Streamable HTTP**:
```
stdin → JSON-RPC message
  ↓
POST /endpoint with JSON-RPC body
  ↓
Parse response (JSON or SSE)
  ↓
stdout ← JSON-RPC message
```

### Problem
This doesn't work with mcp-hub because:
1. ❌ We POST to `/mcp` but mcp-hub expects GET
2. ❌ We don't establish SSE connection first
3. ❌ We don't have a sessionId to use with `/messages`
4. ❌ We try to read response from POST, but responses come via SSE

## Solution: Dual-Mode Proxy

### Option 1: mcp-hub Mode (Recommended)
Add a `--mcp-hub` flag that switches to mcp-hub's transport:

```go
type Proxy struct {
    url       string
    sessionID string
    client    *http.Client
    stdin     *bufio.Scanner
    stdout    io.Writer
    debug     bool
    mcpHubMode bool  // NEW: Enable mcp-hub compatibility
    sseConn   *SSEConnection  // NEW: Long-lived SSE connection
}
```

**Flow**:
1. Establish GET /mcp SSE connection on startup
2. Extract sessionId from transport
3. For each stdin message:
   - POST to /messages?sessionId=xxx
   - Wait for response on SSE stream
4. Match JSON-RPC ID to correlate requests/responses

### Option 2: Auto-Detection
Detect mcp-hub by:
- Attempting standard POST /mcp
- If 404, fall back to GET /mcp + POST /messages pattern
- Store detected mode for session

### Option 3: Separate Binary
Create `mcp-hub-proxy` specifically for mcp-hub's protocol.

## Implementation Plan

### Phase 1: Add mcp-hub Mode Flag
```go
--mcp-hub-mode    Enable mcp-hub compatibility (split SSE transport)
```

### Phase 2: Implement SSE Connection
- Establish GET /mcp connection
- Parse SSE stream in background goroutine
- Maintain sessionId from transport

### Phase 3: Implement Message Routing
- POST to /messages?sessionId=xxx
- Match responses by JSON-RPC ID
- Handle async responses from SSE stream

### Phase 4: Testing
- Test initialization with mcp-hub
- Test tool listing
- Test multi-message sessions
- Verify session persistence

## Technical Challenges

### 1. Async Response Handling
**Problem**: Responses arrive via SSE, not in POST response
**Solution**:
- Channel-based request/response matching
- Map[requestID]responseChan for correlation

### 2. Connection Management
**Problem**: Need to keep GET /mcp connection alive
**Solution**:
- Background goroutine for SSE reading
- Reconnection logic on connection loss
- Graceful shutdown handling

### 3. Session Initialization
**Problem**: Need sessionId before sending messages
**Solution**:
- Wait for SSE connection establishment
- Parse sessionId from transport (may need custom parsing)
- Alternative: Use sessionId from first message if provided

## Compatibility Matrix

| Server Type | Standard Streamable HTTP | mcp-hub Mode |
|-------------|-------------------------|--------------|
| **MCP-compliant server** | ✅ Works | ❌ Wrong protocol |
| **mcp-hub v4.2.1** | ❌ 404 errors | ✅ Works (pending implementation) |
| **Future mcp-hub** | ? (if they add spec compliance) | ✅ Backward compatible |

## References

### Code Locations in mcp-hub
- **SSE endpoint**: `src/server.js:346-358`
- **Message endpoint**: `src/server.js:361-373`
- **Transport handler**: `src/mcp/server.js:475-543`
- **SSE transport**: Uses `@modelcontextprotocol/sdk/server/sse.js`

### MCP SDK Usage
mcp-hub uses the official `@modelcontextprotocol/sdk` package:
```javascript
import { SSEServerTransport } from "@modelcontextprotocol/sdk/server/sse.js";
```

This suggests the SDK itself may support this split-endpoint pattern, which could inform our Go implementation.

## Recommendations

1. **Implement mcp-hub mode** as a flag-based option
2. **Keep standard mode** as default (spec-compliant)
3. **Document clearly** which mode to use with which servers
4. **Auto-detection** can be added later if needed
5. **Upstream feedback**: Consider reporting mcp-hub's non-compliance to help improve the ecosystem

## Notes

- mcp-hub is a popular MCP server aggregator, so compatibility is valuable
- The split-endpoint pattern may be intentional for their use case (managing multiple servers)
- The `@modelcontextprotocol/sdk` supports this, suggesting it's a known pattern
- Future MCP spec versions may formalize this as an alternative transport
