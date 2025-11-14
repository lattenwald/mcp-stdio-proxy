package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestNewHealthChecker tests constructor validation
func TestNewHealthChecker(t *testing.T) {
	proxy := &Proxy{}

	tests := []struct {
		name         string
		interval     time.Duration
		timeout      time.Duration
		recoveryWait time.Duration
		baseURL      string
		expectError  bool
	}{
		{"valid config", 60 * time.Second, 5 * time.Second, 10 * time.Second, "http://localhost", false},
		{"interval too short", 3 * time.Second, 5 * time.Second, 10 * time.Second, "http://localhost", true},
		{"timeout too long", 60 * time.Second, 70 * time.Second, 10 * time.Second, "http://localhost", true},
		{"recovery wait too short", 60 * time.Second, 5 * time.Second, 2 * time.Second, "http://localhost", true},
		{"invalid URL", 60 * time.Second, 5 * time.Second, 10 * time.Second, "not-a-url", true},
		{"nil proxy", 60 * time.Second, 5 * time.Second, 10 * time.Second, "http://localhost", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p *Proxy
			if tt.name != "nil proxy" {
				p = proxy
			}

			_, err := NewHealthChecker(p, tt.interval, tt.timeout, tt.recoveryWait, tt.baseURL, false)
			if (err != nil) != tt.expectError {
				t.Errorf("expected error=%v, got %v", tt.expectError, err)
			}
		})
	}
}

// TestHealthCheckSuccess verifies successful health check with real HTTP server
func TestHealthCheckSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			return
		}

		if r.Method != "GET" {
			t.Errorf("unexpected method: %s", r.Method)
			return
		}

		// Return valid health response
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(HealthResponse{
			State:  "ready",
			Status: "ok",
		})
	}))
	defer server.Close()

	proxy := &Proxy{}
	hc, err := NewHealthChecker(proxy, 60*time.Second, 5*time.Second, 10*time.Second, server.URL, false)
	if err != nil {
		t.Fatalf("failed to create health checker: %v", err)
	}

	if !hc.checkHealth() {
		t.Error("expected health check to pass")
	}
}

// TestHealthCheckFailureHTTPError tests failure with HTTP errors
func TestHealthCheckFailureHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	proxy := &Proxy{}
	hc, err := NewHealthChecker(proxy, 60*time.Second, 5*time.Second, 10*time.Second, server.URL, false)
	if err != nil {
		t.Fatalf("failed to create health checker: %v", err)
	}

	if hc.checkHealth() {
		t.Error("expected health check to fail")
	}
}

// TestHealthCheckFailureInvalidJSON tests failure with malformed JSON
func TestHealthCheckFailureInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{invalid json}"))
	}))
	defer server.Close()

	proxy := &Proxy{}
	hc, err := NewHealthChecker(proxy, 60*time.Second, 5*time.Second, 10*time.Second, server.URL, false)
	if err != nil {
		t.Fatalf("failed to create health checker: %v", err)
	}

	if hc.checkHealth() {
		t.Error("expected health check to fail with invalid JSON")
	}
}

// TestHealthCheckFailureWrongState tests failure when state is not "ready"
func TestHealthCheckFailureWrongState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(HealthResponse{
			State:  "error",
			Status: "ok",
		})
	}))
	defer server.Close()

	proxy := &Proxy{}
	hc, err := NewHealthChecker(proxy, 60*time.Second, 5*time.Second, 10*time.Second, server.URL, false)
	if err != nil {
		t.Fatalf("failed to create health checker: %v", err)
	}

	if hc.checkHealth() {
		t.Error("expected health check to fail with wrong state")
	}
}

// TestHealthStateTransitions tests state machine transitions
func TestHealthStateTransitions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		if r.URL.Path == "/api/restart" {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	proxy := &Proxy{}
	hc, err := NewHealthChecker(proxy, 5*time.Second, 2*time.Second, 5*time.Second, server.URL, false)
	if err != nil {
		t.Fatalf("failed to create health checker: %v", err)
	}

	// Initial state should be Healthy
	if hc.getState() != StateHealthy {
		t.Errorf("expected initial state Healthy, got %v", hc.getState())
	}

	// Simulate failure - this will call attemptRestart() internally
	// which immediately transitions to RestartAttempted
	hc.handleHealthFailure()

	// Verify restart was attempted
	hc.mu.Lock()
	restartAttempted := hc.restartAttempted
	hc.mu.Unlock()
	if !restartAttempted {
		t.Error("expected restart to be attempted")
	}

	// State should be RestartAttempted (not Unhealthy) because
	// handleHealthFailure() calls attemptRestart() immediately
	if hc.getState() != StateRestartAttempted {
		t.Errorf("expected state RestartAttempted after restart, got %v", hc.getState())
	}

	// Another failure should transition to Failed
	hc.handleHealthFailure()
	if hc.getState() != StateFailed {
		t.Errorf("expected state Failed after second failure, got %v", hc.getState())
	}
}

// TestSingleRestartAttempt verifies only one restart attempt is made
func TestSingleRestartAttempt(t *testing.T) {
	restartCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/restart" {
			restartCount++
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	proxy := &Proxy{}
	hc, err := NewHealthChecker(proxy, 5*time.Second, 2*time.Second, 5*time.Second, server.URL, false)
	if err != nil {
		t.Fatalf("failed to create health checker: %v", err)
	}

	// First restart attempt
	hc.attemptRestart()
	if restartCount != 1 {
		t.Errorf("expected 1 restart, got %d", restartCount)
	}

	// Second attempt should be skipped
	hc.attemptRestart()
	if restartCount != 1 {
		t.Errorf("expected still 1 restart after second attempt, got %d", restartCount)
	}
}

// TestRestartFailureHTTPError tests restart failure when API returns error
func TestRestartFailureHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/restart" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("restart failed"))
		}
	}))
	defer server.Close()

	proxy := &Proxy{}
	hc, err := NewHealthChecker(proxy, 5*time.Second, 2*time.Second, 5*time.Second, server.URL, false)
	if err != nil {
		t.Fatalf("failed to create health checker: %v", err)
	}

	hc.attemptRestart()

	// Should transition to Failed state on HTTP error
	if hc.getState() != StateFailed {
		t.Errorf("expected state Failed after restart error, got %v", hc.getState())
	}
}

// TestGracefulShutdown verifies clean shutdown
func TestGracefulShutdown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(HealthResponse{State: "ready", Status: "ok"})
	}))
	defer server.Close()

	proxy := &Proxy{}
	hc, err := NewHealthChecker(proxy, 5*time.Second, 2*time.Second, 5*time.Second, server.URL, false)
	if err != nil {
		t.Fatalf("failed to create health checker: %v", err)
	}

	hc.Start()
	time.Sleep(100 * time.Millisecond) // Brief startup

	// This should not hang
	done := make(chan struct{})
	go func() {
		hc.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success - shutdown completed
	case <-time.After(2 * time.Second):
		t.Error("graceful shutdown timed out")
	}
}

// TestConfigurableRecoveryWait verifies recovery wait time is configurable
func TestConfigurableRecoveryWait(t *testing.T) {
	proxy := &Proxy{}
	customWait := 15 * time.Second

	hc, err := NewHealthChecker(proxy, 60*time.Second, 5*time.Second, customWait, "http://localhost", false)
	if err != nil {
		t.Fatalf("failed to create health checker: %v", err)
	}

	if hc.recoveryWait != customWait {
		t.Errorf("expected recovery wait %v, got %v", customWait, hc.recoveryWait)
	}
}

// TestRecoveryVerification tests the recovery verification flow
func TestRecoveryVerification(t *testing.T) {
	healthyAfterRestart := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			w.Header().Set("Content-Type", "application/json")
			if healthyAfterRestart {
				_ = json.NewEncoder(w).Encode(HealthResponse{State: "ready", Status: "ok"})
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
		}
		if r.URL.Path == "/api/restart" {
			healthyAfterRestart = true // Simulate successful restart
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	proxy := &Proxy{}
	hc, err := NewHealthChecker(proxy, 10*time.Second, 2*time.Second, 5*time.Second, server.URL, false)
	if err != nil {
		t.Fatalf("failed to create health checker: %v", err)
	}

	// Trigger restart
	hc.attemptRestart()

	// Wait for recovery verification (recovery wait + buffer)
	time.Sleep(6 * time.Second)

	// Should have transitioned back to Healthy
	if hc.getState() != StateHealthy {
		t.Errorf("expected state Healthy after successful recovery, got %v", hc.getState())
	}
}

// TestHealthCheckTimeout verifies timeout handling
func TestHealthCheckTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second) // Longer than timeout
		_ = json.NewEncoder(w).Encode(HealthResponse{State: "ready", Status: "ok"})
	}))
	defer server.Close()

	proxy := &Proxy{}
	hc, err := NewHealthChecker(proxy, 10*time.Second, 2*time.Second, 5*time.Second, server.URL, false)
	if err != nil {
		t.Fatalf("failed to create health checker: %v", err)
	}

	// Should timeout and return false
	if hc.checkHealth() {
		t.Error("expected health check to fail due to timeout")
	}
}

// TestPeriodicHealthChecks verifies health checks run periodically
func TestPeriodicHealthChecks(t *testing.T) {
	checkCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			checkCount++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(HealthResponse{State: "ready", Status: "ok"})
		}
	}))
	defer server.Close()

	proxy := &Proxy{}
	// Use minimum allowed interval (5s) - test will be slower but validates real behavior
	hc, err := NewHealthChecker(proxy, 5*time.Second, 2*time.Second, 5*time.Second, server.URL, false)
	if err != nil {
		t.Fatalf("failed to create health checker: %v", err)
	}

	hc.Start()
	// Wait for at least 2 ticks (5s + 5s = 10s, plus buffer for first tick)
	time.Sleep(11 * time.Second)
	hc.Stop()

	// With 5s interval, we should get at least 2 health checks in 11s
	if checkCount < 2 {
		t.Errorf("expected at least 2 health checks in 11s with 5s interval, got %d", checkCount)
	}
}
