package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// HealthChecker manages periodic health checks for mcp-hub
type HealthChecker struct {
	proxy            *Proxy
	interval         time.Duration
	timeout          time.Duration
	recoveryWait     time.Duration
	baseURL          string
	client           *http.Client
	ticker           *time.Ticker
	stopChan         chan struct{}
	doneChan         chan struct{}
	state            HealthState
	restartAttempted bool
	debug            bool
	mu               sync.Mutex // protects state and restartAttempted
}

// HealthState represents the current health status
type HealthState int

const (
	StateHealthy HealthState = iota
	StateUnhealthy
	StateRestartAttempted
	StateFailed
)

// String returns human-readable state name
func (s HealthState) String() string {
	return [...]string{"Healthy", "Unhealthy", "RestartAttempted", "Failed"}[s]
}

// getState returns the current state (thread-safe)
func (h *HealthChecker) getState() HealthState {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.state
}

// HealthResponse represents /api/health JSON response
type HealthResponse struct {
	State  string `json:"state"`  // Expected: "ready"
	Status string `json:"status"` // Expected: "ok"
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(
	proxy *Proxy,
	interval time.Duration,
	timeout time.Duration,
	recoveryWait time.Duration,
	baseURL string,
	debug bool,
) (*HealthChecker, error) {
	if proxy == nil {
		return nil, fmt.Errorf("proxy cannot be nil")
	}

	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		return nil, fmt.Errorf("invalid base URL: %s", baseURL)
	}

	if interval < 5*time.Second {
		return nil, fmt.Errorf("health check interval must be at least 5 seconds")
	}

	if timeout < 1*time.Second || timeout >= interval {
		return nil, fmt.Errorf("health check timeout must be 1s to %v", interval-time.Second)
	}

	if recoveryWait < 5*time.Second {
		return nil, fmt.Errorf("recovery wait must be at least 5 seconds")
	}

	client := &http.Client{Timeout: timeout}

	return &HealthChecker{
		proxy:        proxy,
		interval:     interval,
		timeout:      timeout,
		recoveryWait: recoveryWait,
		baseURL:      baseURL,
		client:       client,
		stopChan:     make(chan struct{}),
		doneChan:     make(chan struct{}),
		state:        StateHealthy,
		debug:        debug,
	}, nil
}

func (h *HealthChecker) Start() {
	h.debugLog("Starting health checker (interval: %v, recovery wait: %v)",
		h.interval, h.recoveryWait)
	h.ticker = time.NewTicker(h.interval)
	go h.run()
}

func (h *HealthChecker) Stop() {
	h.debugLog("Stopping health checker")
	close(h.stopChan)
	<-h.doneChan
	h.debugLog("Health checker stopped")
}

func (h *HealthChecker) run() {
	defer close(h.doneChan)
	defer h.ticker.Stop()

	for {
		select {
		case <-h.ticker.C:
			h.performCheck()
		case <-h.stopChan:
			return
		}
	}
}

func (h *HealthChecker) debugLog(format string, args ...interface{}) {
	if h.debug {
		log.Printf("[HEALTH] "+format, args...)
	}
}

func (h *HealthChecker) performCheck() {
	h.mu.Lock()
	currentState := h.state
	h.mu.Unlock()

	h.debugLog("Performing health check (state: %s)", currentState)

	h.mu.Lock()
	if h.state == StateFailed {
		h.mu.Unlock()
		h.debugLog("Skipping check (in failed state)")
		return
	}
	h.mu.Unlock()

	healthy := h.checkHealth()

	if healthy {
		h.handleHealthSuccess()
	} else {
		h.handleHealthFailure()
	}
}

func (h *HealthChecker) checkHealth() bool {
	url := h.baseURL + "/api/health"

	ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		h.debugLog("Failed to create health check request: %v", err)
		return false
	}

	resp, err := h.client.Do(req)
	if err != nil {
		h.debugLog("Health check request failed: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		h.debugLog("Health check returned status %d", resp.StatusCode)
		return false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		h.debugLog("Failed to read health response: %v", err)
		return false
	}

	var health HealthResponse
	if err := json.Unmarshal(body, &health); err != nil {
		h.debugLog("Failed to parse health response: %v", err)
		return false
	}

	if health.State != "ready" || health.Status != "ok" {
		h.debugLog("Health check failed: state=%s, status=%s", health.State, health.Status)
		return false
	}

	h.debugLog("Health check passed")
	return true
}

func (h *HealthChecker) handleHealthSuccess() {
	h.mu.Lock()
	defer h.mu.Unlock()

	oldState := h.state

	if h.state == StateUnhealthy || h.state == StateRestartAttempted {
		h.state = StateHealthy
		h.debugLog("State transition: %s -> %s (recovered)", oldState, h.state)
		if oldState == StateRestartAttempted {
			log.Printf("[HEALTH] mcp-hub restart successful, service recovered")
		}
	}
}

func (h *HealthChecker) handleHealthFailure() {
	h.mu.Lock()
	oldState := h.state

	switch h.state {
	case StateHealthy:
		h.state = StateUnhealthy
		h.debugLog("State transition: %s -> %s", oldState, h.state)
		h.mu.Unlock()
		log.Printf("[HEALTH] mcp-hub health check failed, attempting restart...")
		h.attemptRestart()

	case StateRestartAttempted:
		h.state = StateFailed
		h.debugLog("State transition: %s -> %s", oldState, h.state)
		h.mu.Unlock()
		log.Printf("[HEALTH] ERROR: mcp-hub restart verification failed, giving up")
		log.Printf("[HEALTH] Health monitoring disabled. Manual intervention required.")

	case StateUnhealthy:
		h.debugLog("Health check failed while in Unhealthy state (unexpected)")
		h.mu.Unlock()

	default:
		h.mu.Unlock()
	}
}

// Triggers /api/restart endpoint
func (h *HealthChecker) attemptRestart() {
	h.mu.Lock()
	if h.restartAttempted {
		h.mu.Unlock()
		h.debugLog("Skipping restart (already attempted)")
		return
	}

	h.restartAttempted = true
	h.mu.Unlock()

	url := h.baseURL + "/api/restart"

	h.debugLog("Sending restart request to %s", url)

	ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		log.Printf("[HEALTH] Failed to create restart request: %v", err)
		h.mu.Lock()
		h.state = StateFailed
		h.mu.Unlock()
		return
	}

	resp, err := h.client.Do(req)
	if err != nil {
		log.Printf("[HEALTH] Restart request failed: %v", err)
		h.mu.Lock()
		h.state = StateFailed
		h.mu.Unlock()
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[HEALTH] Restart request returned HTTP %d: %s", resp.StatusCode, string(body))
		h.mu.Lock()
		h.state = StateFailed
		h.mu.Unlock()
		return
	}

	h.debugLog("Restart request successful (HTTP %d)", resp.StatusCode)
	h.mu.Lock()
	h.state = StateRestartAttempted
	h.mu.Unlock()

	go h.verifyRecovery()
}

func (h *HealthChecker) verifyRecovery() {
	h.debugLog("Waiting %v before verifying recovery...", h.recoveryWait)

	select {
	case <-time.After(h.recoveryWait):
		h.debugLog("Verifying mcp-hub recovery...")
		healthy := h.checkHealth()

		h.mu.Lock()
		if healthy {
			h.state = StateHealthy
			h.mu.Unlock()
			log.Printf("[HEALTH] mcp-hub restart successful, service recovered")
		} else {
			h.mu.Unlock()
			h.debugLog("Recovery verification failed, waiting for next check")
		}
	case <-h.stopChan:
		h.debugLog("Recovery verification cancelled (shutdown)")
	}
}
