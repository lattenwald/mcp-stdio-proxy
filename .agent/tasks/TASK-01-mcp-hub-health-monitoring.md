# TASK-01: MCP-Hub Health Monitoring with Auto-Restart

**Status**: 🚧 In Progress
**Created**: 2025-11-14
**Assignee**: Manual

---

## Context

**Problem**:
mcp-hub sometimes stops working or enters an unhealthy state, requiring manual intervention to detect and restart. This disrupts the proxy's ability to forward MCP requests, causing failures for clients.

**Goal**:
Implement periodic health monitoring that automatically detects when mcp-hub is unhealthy and attempts a soft restart. After one failed restart attempt, stop trying and log an error to prevent infinite restart loops.

**Success Criteria**:
- [ ] Health check runs periodically (default: 60 seconds, configurable)
- [ ] When health check fails, log the issue and trigger soft restart via `/api/restart`
- [ ] If health check fails again after restart, log error and stop restart attempts
- [ ] Configurable via CLI flags: `--health-check-interval` and `--enable-health-check`
- [ ] Clean shutdown of health monitoring goroutine on proxy shutdown

---

## Implementation Plan

### Phase 1: Health Check Client
**Goal**: Implement HTTP client to check mcp-hub health endpoint

**Tasks**:
- [ ] Create `healthcheck.go` with `HealthChecker` struct
- [ ] Implement `CheckHealth(url string) (bool, error)` function
- [ ] Parse `/api/health` JSON response
- [ ] Check for `state: "ready"` and `status: "ok"`
- [ ] Add timeout handling (5 seconds default)

**Files**:
- `healthcheck.go` (new) - Health check client implementation

### Phase 2: Periodic Monitoring Goroutine
**Goal**: Run health checks on a timer in background goroutine

**Tasks**:
- [ ] Add health check configuration to `Proxy` struct
- [ ] Implement `StartHealthMonitoring()` method
- [ ] Use `time.Ticker` for periodic checks (60s default)
- [ ] Handle graceful shutdown via context cancellation
- [ ] Track restart attempt state (attempted/not attempted)

**Files**:
- `main.go` - Add health monitoring to Proxy struct and Run() method
- `healthcheck.go` - Monitoring goroutine implementation

### Phase 3: Auto-Restart Logic
**Goal**: Trigger soft restart on health check failure

**Tasks**:
- [ ] Implement `SoftRestart(url string) error` function
- [ ] Make POST request to `/api/restart` endpoint
- [ ] Add retry state tracking (max 1 restart attempt)
- [ ] Log health failures and restart attempts
- [ ] Stop monitoring after second failure

**Files**:
- `healthcheck.go` - Restart logic
- `main.go` - Integration with proxy lifecycle

### Phase 4: CLI Configuration
**Goal**: Make health monitoring configurable via command-line flags

**Tasks**:
- [ ] Add `--enable-health-check` flag (default: false)
- [ ] Add `--health-check-interval` flag (default: 60s)
- [ ] Add `--health-check-timeout` flag (default: 5s)
- [ ] Update help message with new flags
- [ ] Add debug logging for health check results

**Files**:
- `main.go` - CLI flag parsing and help text

### Phase 5: Testing
**Goal**: Verify health monitoring works correctly

**Tasks**:
- [ ] Test with healthy mcp-hub instance
- [ ] Test with stopped mcp-hub instance
- [ ] Test soft restart functionality
- [ ] Test double-failure scenario (no infinite restarts)
- [ ] Test graceful shutdown of monitoring goroutine
- [ ] Verify debug logging output

**Files**:
- Manual testing with running mcp-hub instance

---

## Technical Decisions

| Decision | Options Considered | Chosen | Reasoning |
|----------|-------------------|--------|-----------|
| Health check interval | 30s, 60s, 120s | 60s default | Balance between responsiveness and API load |
| Restart limit | 0, 1, 3, infinite | 1 attempt | Prevent infinite restart loops, require manual intervention if restart fails |
| Restart method | Kill & restart, soft restart API | Soft restart API (`/api/restart`) | Cleaner, preserves hub process, reloads config |
| Default behavior | Enabled by default, opt-in | Opt-in (`--enable-health-check`) | Conservative approach, user must explicitly enable |
| Concurrency | Goroutine, separate process | Goroutine with ticker | Simpler, shares proxy context, easy shutdown |

---

## API Endpoints Used

### Health Check
```bash
GET http://localhost:37373/api/health
```

**Expected Response (Healthy)**:
```json
{
  "status": "ok",
  "state": "ready",
  "server_id": "mcp-hub",
  "version": "4.1.1",
  "activeClients": 2,
  "timestamp": "2024-02-20T05:55:00.000Z"
}
```

### Soft Restart
```bash
POST http://localhost:37373/api/restart
```

**Expected Response**:
```json
{
  "status": "ok",
  "timestamp": "2024-02-20T05:55:00.000Z"
}
```

---

## Dependencies

**Requires**:
- [ ] mcp-hub instance running with `/api/health` and `/api/restart` endpoints
- [ ] Go stdlib packages: `net/http`, `time`, `context`, `encoding/json`

**Blocks**:
- None (optional feature, doesn't block other functionality)

---

## Implementation Details

### Health Check States

```go
type HealthState int

const (
    StateHealthy HealthState = iota
    StateUnhealthy
    StateRestartAttempted
    StateFailedAfterRestart
)
```

### Monitoring Flow

```
┌─────────────────┐
│  Start Monitor  │
└────────┬────────┘
         │
         ▼
    ┌────────┐
    │  Wait  │◄────────────┐
    │  60s   │             │
    └────┬───┘             │
         │                 │
         ▼                 │
   ┌──────────┐            │
   │  Check   │            │
   │  Health  │            │
   └─────┬────┘            │
         │                 │
    ┌────▼─────┐           │
    │ Healthy? │           │
    └────┬─────┘           │
         │                 │
    ┌────▼──────┬─────┐    │
    │    Yes    │ No  │    │
    └────┬──────┴──┬──┘    │
         │         │       │
         │    ┌────▼────┐  │
         │    │  Log &  │  │
         │    │ Restart │  │
         │    └────┬────┘  │
         │         │       │
         │    ┌────▼────┐  │
         │    │  Check  │  │
         │    │  Again  │  │
         │    └────┬────┘  │
         │         │       │
         │    ┌────▼─────┐ │
         │    │ Healthy? │ │
         │    └────┬─────┘ │
         │         │       │
         │    ┌────▼──┬──┐ │
         │    │  Yes  │No│ │
         │    └───┬───┴┬─┘ │
         │        │    │   │
         │        │  ┌─▼──────┐
         │        │  │  Log   │
         │        │  │ Error  │
         │        │  │  Stop  │
         │        │  └────────┘
         │        │
         └────────┘
```

---

## Logging Examples

**Healthy Check**:
```
[DEBUG] Health check passed (state: ready, clients: 2)
```

**First Failure**:
```
[WARN] Health check failed: state=error, attempting soft restart...
[INFO] Triggered soft restart via /api/restart
[INFO] Waiting 10s before next health check...
```

**Recovery After Restart**:
```
[INFO] Health check passed after restart (state: ready)
```

**Failure After Restart**:
```
[ERROR] Health check failed after restart attempt. Manual intervention required.
[ERROR] Health monitoring stopped. Please check mcp-hub status manually.
```

---

## Completion Checklist

Before marking complete:
- [ ] Implementation finished in `healthcheck.go` and `main.go`
- [ ] All CLI flags work correctly
- [ ] Tested healthy, unhealthy, and restart scenarios
- [ ] Debug logging provides useful information
- [ ] Graceful shutdown works (no goroutine leaks)
- [ ] README.md updated with health monitoring documentation
- [ ] Code follows Go idioms and project conventions

---

## Notes

**Alternative Approaches Considered**:
- **External monitoring script**: Would work but adds deployment complexity
- **Systemd watchdog**: Linux-specific, wouldn't work on all platforms
- **Built-in monitoring**: Chosen for simplicity and cross-platform support

**Future Enhancements** (not in scope for this task):
- Exponential backoff for restart timing
- Metrics export (Prometheus/StatsD)
- Multiple restart attempts with backoff
- Email/Slack notifications on failure
- Health check history tracking

**Related Documentation**:
- mcp-hub README: `/api/health` and `/api/restart` endpoints
- Go time.Ticker: https://pkg.go.dev/time#Ticker

---

**Last Updated**: 2025-11-14
