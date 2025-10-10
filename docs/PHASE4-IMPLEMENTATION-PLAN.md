# Phase 4 Implementation Plan: mcp-hub Compatibility Mode

**Objective**: Add `--mcp-hub` flag to support mcp-hub's split-endpoint SSE transport.

**Estimated Effort**: 6-7 hours

## Prerequisites

Read these documents first:
- `docs/MCP-HUB-QUIRKS.md` - Understand mcp-hub's protocol
- `main.go` - Current implementation structure

## Protocol Overview

**mcp-hub transport pattern**:
1. `GET /mcp` → Establishes SSE connection, returns sessionId
2. `POST /messages?sessionId=xxx` → Sends JSON-RPC messages (returns 200 OK ack)
3. SSE stream → All responses arrive via SSE, matched by JSON-RPC ID

**Key difference from standard mode**: Responses come asynchronously via SSE, not from POST response.

## Implementation Checklist

### Step 1: Add Import and Struct Fields

**File**: `main.go`

**Add to imports** (line ~3):
```go
import (
    // ... existing imports ...
    "sync"  // NEW
)
```

**Extend Proxy struct** (line ~17):
```go
type Proxy struct {
    // Existing fields
    url       string
    sessionID string
    client    *http.Client
    stdin     *bufio.Scanner
    stdout    io.Writer
    debug     bool

    // NEW: mcp-hub mode fields
    mcpHubMode   bool
    sseConn      *http.Response
    sseReader    *bufio.Scanner
    pendingReqs  map[string]chan *JSONRPCMessage
    pendingMutex sync.RWMutex
    sseError     chan error
    shutdown     chan struct{}
}
```

---

### Step 2: Add CLI Flag

**Location**: `main() function` (line ~43)

**Add flag definition** (after existing flags):
```go
mcpHubFlag := flag.Bool("mcp-hub", false, "Enable mcp-hub compatibility mode (split SSE transport)")
```

**Update usage message** (in `flag.Usage` function):
```go
fmt.Fprintf(os.Stderr, "  -mcp-hub\n")
fmt.Fprintf(os.Stderr, "    \tEnable mcp-hub compatibility mode (split SSE transport)\n")
```

**Add example** (in examples section):
```go
fmt.Fprintf(os.Stderr, "  %s --mcp-hub http://localhost:37373\n", os.Args[0])
```

**Pass to Proxy** (line ~86):
```go
proxy := &Proxy{
    // ... existing fields ...
    mcpHubMode: *mcpHubFlag,  // NEW
}
```

---

### Step 3: Add Mode Switching to Run()

**Location**: `Run()` function (line ~107)

**Replace entire function with**:
```go
func (p *Proxy) Run() error {
    if p.mcpHubMode {
        return p.RunMCPHubMode()
    }

    // EXISTING CODE BELOW (don't change)
    for p.stdin.Scan() {
        // ... (keep all existing code)
    }
    // ... rest of existing function
}
```

---

### Step 4: Implement RunMCPHubMode()

**Location**: Add new function after `Run()`

```go
// RunMCPHubMode implements mcp-hub's split-endpoint SSE transport
func (p *Proxy) RunMCPHubMode() error {
    // Initialize structures
    p.pendingReqs = make(map[string]chan *JSONRPCMessage)
    p.sseError = make(chan error, 1)
    p.shutdown = make(chan struct{})

    // Setup cleanup
    defer p.shutdownMCPHub()

    // Establish SSE connection
    if err := p.establishSSEConnection(); err != nil {
        return fmt.Errorf("failed to establish SSE connection: %w", err)
    }

    if p.debug {
        log.Printf("[MCP-HUB] Connected, sessionId: %s", p.sessionID)
    }

    // Start background SSE reader
    go p.readSSEStream()

    // Main stdin loop
    for p.stdin.Scan() {
        line := p.stdin.Text()
        if line == "" {
            continue
        }

        if p.debug {
            log.Printf("[STDIN] Received: %s", line)
        }

        // Parse JSON-RPC message
        var msg JSONRPCMessage
        if err := json.Unmarshal([]byte(line), &msg); err != nil {
            log.Printf("[ERROR] Invalid JSON-RPC message: %v", err)
            continue
        }

        // Send request via mcp-hub transport
        if err := p.sendMCPHubRequest(line, &msg); err != nil {
            log.Printf("[ERROR] Failed to send request: %v", err)
            if msg.ID != nil {
                p.sendErrorResponse(msg.ID, -32603, fmt.Sprintf("Request failed: %v", err))
            }
        }
    }

    return p.stdin.Err()
}
```

---

### Step 5: Implement establishSSEConnection()

**Location**: Add new function after `RunMCPHubMode()`

```go
// establishSSEConnection connects to mcp-hub's SSE endpoint and extracts sessionId
func (p *Proxy) establishSSEConnection() error {
    req, err := http.NewRequest("GET", p.url, nil)
    if err != nil {
        return fmt.Errorf("failed to create request: %w", err)
    }

    if p.debug {
        log.Printf("[MCP-HUB] Connecting to SSE: GET %s", p.url)
    }

    resp, err := p.client.Do(req)
    if err != nil {
        return fmt.Errorf("SSE connection failed: %w", err)
    }

    // Verify content type
    contentType := resp.Header.Get("Content-Type")
    if !strings.Contains(contentType, "text/event-stream") {
        resp.Body.Close()
        return fmt.Errorf("expected text/event-stream, got: %s", contentType)
    }

    // Try to get sessionId from headers first
    sessionID := resp.Header.Get("X-Session-Id")
    if sessionID == "" {
        sessionID = resp.Header.Get("Mcp-Session-Id")
    }

    // If not in headers, read first SSE event
    if sessionID == "" {
        scanner := bufio.NewScanner(resp.Body)
        var err error
        sessionID, err = p.extractSessionIdFromSSE(scanner)
        if err != nil {
            resp.Body.Close()
            return fmt.Errorf("failed to extract sessionId: %w", err)
        }
        p.sseReader = scanner
    } else {
        p.sseReader = bufio.NewScanner(resp.Body)
    }

    p.sessionID = sessionID
    p.sseConn = resp

    return nil
}

// extractSessionIdFromSSE reads SSE stream until sessionId is found
func (p *Proxy) extractSessionIdFromSSE(scanner *bufio.Scanner) (string, error) {
    // Read up to 10 SSE events looking for sessionId
    for i := 0; i < 10; i++ {
        if !scanner.Scan() {
            return "", fmt.Errorf("SSE stream ended before sessionId found")
        }

        line := scanner.Text()

        // Look for endpoint message with sessionId
        // Example: data: {"endpoint":"/messages?sessionId=xxx"}
        if strings.HasPrefix(line, "data: ") {
            data := strings.TrimPrefix(line, "data: ")

            // Try to extract sessionId from various formats
            var msg map[string]interface{}
            if err := json.Unmarshal([]byte(data), &msg); err == nil {
                // Check for endpoint field
                if endpoint, ok := msg["endpoint"].(string); ok {
                    if strings.Contains(endpoint, "sessionId=") {
                        parts := strings.Split(endpoint, "sessionId=")
                        if len(parts) > 1 {
                            return parts[1], nil
                        }
                    }
                }

                // Check for direct sessionId field
                if sid, ok := msg["sessionId"].(string); ok {
                    return sid, nil
                }
            }
        }
    }

    return "", fmt.Errorf("sessionId not found in first 10 SSE events")
}
```

**NOTE**: The sessionId extraction logic may need adjustment based on actual mcp-hub behavior. Test and iterate if needed.

---

### Step 6: Implement readSSEStream()

**Location**: Add new function after `extractSessionIdFromSSE()`

```go
// readSSEStream runs in background, reading SSE events and routing responses
func (p *Proxy) readSSEStream() {
    defer func() {
        if p.debug {
            log.Printf("[SSE] Reader goroutine exiting")
        }
    }()

    var dataLines []string

    for {
        select {
        case <-p.shutdown:
            return
        default:
            if !p.sseReader.Scan() {
                // Connection lost or error
                if err := p.sseReader.Err(); err != nil {
                    log.Printf("[SSE] Read error: %v", err)
                    select {
                    case p.sseError <- err:
                    default:
                    }
                }
                return
            }

            line := p.sseReader.Text()

            // SSE event boundary (empty line)
            if line == "" {
                if len(dataLines) > 0 {
                    jsonData := strings.Join(dataLines, "\n")
                    p.handleSSEMessage(jsonData)
                    dataLines = nil
                }
                continue
            }

            // Data line
            if strings.HasPrefix(line, "data: ") {
                data := strings.TrimPrefix(line, "data: ")
                dataLines = append(dataLines, data)
            } else if strings.HasPrefix(line, ":") {
                // Comment, ignore
                if p.debug {
                    log.Printf("[SSE] Comment: %s", line)
                }
            } else if strings.HasPrefix(line, "event: ") {
                // Event type
                if p.debug {
                    log.Printf("[SSE] Event: %s", strings.TrimPrefix(line, "event: "))
                }
            }
        }
    }
}

// handleSSEMessage processes a complete SSE message
func (p *Proxy) handleSSEMessage(data string) {
    if p.debug {
        log.Printf("[SSE] Received: %s", data)
    }

    // Parse JSON-RPC message
    var msg JSONRPCMessage
    if err := json.Unmarshal([]byte(data), &msg); err != nil {
        log.Printf("[ERROR] Invalid JSON in SSE: %v", err)
        return
    }

    // Extract ID and find pending request
    if msg.ID == nil {
        // Notification, not a response
        if p.debug {
            log.Printf("[SSE] Received notification (no ID)")
        }
        return
    }

    idStr := string(msg.ID)

    p.pendingMutex.RLock()
    ch, exists := p.pendingReqs[idStr]
    p.pendingMutex.RUnlock()

    if exists {
        // Send to waiting goroutine
        select {
        case ch <- &msg:
            if p.debug {
                log.Printf("[SSE] Matched response for ID: %s", idStr)
            }
        case <-time.After(1 * time.Second):
            log.Printf("[WARN] Timeout sending to channel for ID: %s", idStr)
        }
    } else {
        log.Printf("[WARN] Received response for unknown ID: %s", idStr)
    }
}
```

---

### Step 7: Implement sendMCPHubRequest()

**Location**: Add new function after `handleSSEMessage()`

```go
// sendMCPHubRequest sends a request via mcp-hub's split transport
func (p *Proxy) sendMCPHubRequest(rawMessage string, msg *JSONRPCMessage) error {
    if msg.ID == nil {
        // Notification, no response expected
        return p.postToMessages(rawMessage)
    }

    // Create response channel
    respCh := make(chan *JSONRPCMessage, 1)
    idStr := string(msg.ID)

    // Register pending request
    p.pendingMutex.Lock()
    p.pendingReqs[idStr] = respCh
    p.pendingMutex.Unlock()

    // Cleanup on exit
    defer func() {
        p.pendingMutex.Lock()
        delete(p.pendingReqs, idStr)
        p.pendingMutex.Unlock()
        close(respCh)
    }()

    // POST to /messages endpoint
    if err := p.postToMessages(rawMessage); err != nil {
        return err
    }

    // Wait for response from SSE stream
    timeout := time.After(60 * time.Second)
    select {
    case response := <-respCh:
        // Write response to stdout
        data, err := json.Marshal(response)
        if err != nil {
            return fmt.Errorf("failed to marshal response: %w", err)
        }

        fmt.Fprintf(p.stdout, "%s\n", data)

        if p.debug {
            log.Printf("[STDOUT] Sent response for ID: %s", idStr)
        }

        return nil

    case <-timeout:
        return fmt.Errorf("timeout waiting for response (60s)")
    }
}

// postToMessages sends POST request to /messages endpoint
func (p *Proxy) postToMessages(body string) error {
    // Build /messages URL
    messagesURL := p.buildMessagesURL()

    req, err := http.NewRequest("POST", messagesURL, strings.NewReader(body))
    if err != nil {
        return fmt.Errorf("failed to create request: %w", err)
    }

    req.Header.Set("Content-Type", "application/json")

    if p.debug {
        log.Printf("[HTTP] POST %s", messagesURL)
    }

    resp, err := p.client.Do(req)
    if err != nil {
        return fmt.Errorf("POST failed: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode >= 400 {
        bodyBytes, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
    }

    return nil
}

// buildMessagesURL constructs the /messages endpoint URL with sessionId
func (p *Proxy) buildMessagesURL() string {
    // Parse base URL
    baseURL := strings.TrimSuffix(p.url, "/")

    // Determine messages path
    // If URL is http://host:port/mcp → http://host:port/messages?sessionId=xxx
    // If URL is http://host:port/ → http://host:port/messages?sessionId=xxx

    // Simple approach: replace last path segment or append
    if strings.HasSuffix(baseURL, "/mcp") {
        baseURL = strings.TrimSuffix(baseURL, "/mcp")
    }

    return fmt.Sprintf("%s/messages?sessionId=%s", baseURL, p.sessionID)
}
```

---

### Step 8: Implement Shutdown

**Location**: Add new function after `buildMessagesURL()`

```go
// shutdownMCPHub cleanly shuts down mcp-hub mode connections
func (p *Proxy) shutdownMCPHub() {
    if p.debug {
        log.Printf("[MCP-HUB] Shutting down")
    }

    // Signal shutdown
    if p.shutdown != nil {
        close(p.shutdown)
    }

    // Close SSE connection
    if p.sseConn != nil {
        p.sseConn.Body.Close()
    }

    // Fail all pending requests
    p.pendingMutex.Lock()
    for id, ch := range p.pendingReqs {
        errorMsg := &JSONRPCMessage{
            JSONRPC: "2.0",
            ID:      json.RawMessage(id),
            Error: &JSONRPCError{
                Code:    -32603,
                Message: "Connection closed during shutdown",
            },
        }

        select {
        case ch <- errorMsg:
        default:
        }
    }
    p.pendingMutex.Unlock()
}
```

---

## Testing Instructions

### Build
```bash
go build -o mcp-stdio-proxy
```

### Test 1: Initialization
```bash
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}' | ./mcp-stdio-proxy --mcp-hub --debug http://localhost:37373
```

**Expected**:
- `[MCP-HUB] Connected, sessionId: <some-id>`
- JSON response with initialization result

### Test 2: Tools List
```bash
echo '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' | ./mcp-stdio-proxy --mcp-hub http://localhost:37373
```

**Expected**:
- JSON response with tools array

### Test 3: Multiple Messages
```bash
(echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}'; sleep 0.5; echo '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}') | ./mcp-stdio-proxy --mcp-hub http://localhost:37373
```

**Expected**:
- Two JSON responses in order

### Test 4: Standard Mode Still Works
```bash
echo '{"jsonrpc":"2.0","id":1,"method":"test"}' | ./mcp-stdio-proxy http://example.com/mcp
```

**Expected**:
- Should behave exactly as before (no regression)

---

## Debugging Tips

1. **Always use `--debug` flag during development**
2. **Check sessionId extraction**: If fails, inspect actual SSE stream from mcp-hub
3. **Response matching**: Ensure JSON-RPC IDs are correctly extracted (check for quotes, numbers)
4. **Timeout issues**: Increase timeout in `sendMCPHubRequest()` if needed
5. **Memory leaks**: Monitor with `go tool pprof` if running long sessions

---

## Success Criteria

- [ ] `--mcp-hub` flag recognized
- [ ] SSE connection established
- [ ] sessionId extracted successfully
- [ ] Messages POST to correct endpoint
- [ ] Responses arrive via SSE and match by ID
- [ ] Multiple sequential messages work
- [ ] Timeout handling works
- [ ] Graceful shutdown works
- [ ] Standard mode unchanged
- [ ] No memory leaks after 100+ messages

---

## Post-Implementation Tasks

1. Update `README.md` with `--mcp-hub` examples
2. Update `docs/PROGRESS.md` - mark Phase 4 complete
3. Update `AGENTS.md` - update status
4. Commit with message: `Implement Phase 4: Add mcp-hub compatibility mode`
