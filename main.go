package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Proxy handles the stdio to Streamable HTTP bridge
type Proxy struct {
	url       string
	sessionID string
	client    *http.Client
	stdin     *bufio.Scanner
	stdout    io.Writer
	debug     bool
}

// JSONRPCMessage represents a JSON-RPC 2.0 message
type JSONRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC error object
type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func main() {
	// Define flags
	debugFlag := flag.Bool("debug", false, "Enable debug logging")
	verboseFlag := flag.Bool("v", false, "Enable verbose logging (alias for --debug)")
	flag.BoolVar(verboseFlag, "verbose", false, "Enable verbose logging (alias for --debug)")
	timeoutFlag := flag.Int("timeout", 120, "HTTP request timeout in seconds")
	mcpHubFlag := flag.Bool("mcp-hub", false, "Auto-discover local mcp-hub port")
	mcpHubConfigFlag := flag.String("mcp-hub-config", "", "Display mcp-hub config path (internal use)")

	// Custom usage message
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS] [<streamable-http-url>]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "A minimal stdio to Streamable HTTP proxy for Model Context Protocol (MCP).\n\n")
		fmt.Fprintf(os.Stderr, "Arguments:\n")
		fmt.Fprintf(os.Stderr, "  <streamable-http-url>  Target MCP server URL (required unless --mcp-hub is used)\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s http://localhost:37373/mcp\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --debug http://localhost:37373/mcp\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --timeout 300 http://localhost:37373/mcp\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --mcp-hub\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --mcp-hub --debug\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nEnvironment Variables:\n")
		fmt.Fprintf(os.Stderr, "  DEBUG=1  Alternative way to enable debug logging\n")
	}

	// Parse flags
	flag.Parse()

	// Check for debug mode (flag or environment variable)
	debug := *debugFlag || *verboseFlag || os.Getenv("DEBUG") == "1"

	var url string

	// Handle --mcp-hub mode
	if *mcpHubFlag && flag.NArg() == 0 {
		// First execution: discover and re-exec
		instance, err := discoverMcpHubInstance(debug)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Failed to discover mcp-hub port: %v\n", err)
			os.Exit(1)
		}

		url = fmt.Sprintf("http://localhost:%s/mcp", instance.Port)

		if debug {
			log.SetOutput(os.Stderr)
			log.Printf("[REEXEC] Re-executing with --mcp-hub-config %s %s", instance.ConfigPath, url)
		}

		// Build new args for re-execution
		newArgs := []string{os.Args[0]}

		// Preserve flags
		if *debugFlag {
			newArgs = append(newArgs, "--debug")
		} else if *verboseFlag {
			newArgs = append(newArgs, "--verbose")
		}

		// Add display config
		newArgs = append(newArgs, "--mcp-hub-config", instance.ConfigPath)

		// Add discovered URL
		newArgs = append(newArgs, url)

		// Re-exec
		err = syscall.Exec(os.Args[0], newArgs, os.Environ())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Failed to re-execute: %v\n", err)
			os.Exit(1)
		}
		// Never reaches here
	} else if flag.NArg() == 1 {
		// URL provided (either explicit or after re-exec)
		url = flag.Arg(0)

		// Validate URL
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			fmt.Fprintf(os.Stderr, "Error: URL must start with http:// or https://\n")
			os.Exit(1)
		}

		if debug && *mcpHubConfigFlag != "" {
			log.SetOutput(os.Stderr)
			log.Printf("[INIT] Using mcp-hub config: %s", *mcpHubConfigFlag)
		}
	} else {
		// Invalid usage
		flag.Usage()
		os.Exit(1)
	}

	// Create proxy
	stdinScanner := bufio.NewScanner(os.Stdin)
	// Increase buffer size to handle large JSON-RPC messages (default is 64KB)
	// 1MB should handle even very large tool lists and resource contents
	stdinScanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	proxy := &Proxy{
		url: url,
		client: &http.Client{
			Timeout: time.Duration(*timeoutFlag) * time.Second,
		},
		stdin:  stdinScanner,
		stdout: os.Stdout,
		debug:  debug,
	}

	if proxy.debug {
		log.SetOutput(os.Stderr)
		log.Printf("[INIT] Starting mcp-stdio-proxy, target: %s", url)
	}

	// Run the proxy
	if err := proxy.Run(); err != nil {
		log.Fatalf("Proxy error: %v", err)
	}
}

// Run starts the proxy main loop
func (p *Proxy) Run() error {
	// Read messages from stdin
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

		// Forward to HTTP endpoint
		if err := p.forwardMessage(line, &msg); err != nil {
			log.Printf("[ERROR] Failed to forward message: %v", err)
			// Send error response back to client
			if msg.ID != nil {
				p.sendErrorResponse(msg.ID, -32603, fmt.Sprintf("Internal error: %v", err))
			}
		}
	}

	if err := p.stdin.Err(); err != nil {
		return fmt.Errorf("stdin error: %w", err)
	}

	return nil
}

// forwardMessage sends a message to the HTTP endpoint and handles the response
func (p *Proxy) forwardMessage(rawMessage string, msg *JSONRPCMessage) error {
	var lastErr error
	maxRetries := 3
	backoff := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			if p.debug {
				log.Printf("[RETRY] Attempt %d/%d after %v", attempt+1, maxRetries, backoff[attempt-1])
			}
			time.Sleep(backoff[attempt-1])
		}

		err := p.sendHTTPRequest(rawMessage)
		if err == nil {
			return nil
		}

		lastErr = err
		if p.debug {
			log.Printf("[ERROR] Attempt %d failed: %v", attempt+1, err)
		}
	}

	return fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}

// sendHTTPRequest sends a single HTTP POST request
func (p *Proxy) sendHTTPRequest(body string) error {
	// Create HTTP request
	req, err := http.NewRequest("POST", p.url, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	// Add session ID if we have one
	if p.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", p.sessionID)
		if p.debug {
			log.Printf("[HTTP] Using session ID: %s", p.sessionID)
		}
	}

	if p.debug {
		log.Printf("[HTTP] POST %s", p.url)
	}

	// Send request
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Extract session ID from response if present
	if sessionID := resp.Header.Get("Mcp-Session-Id"); sessionID != "" {
		if p.sessionID == "" {
			p.sessionID = sessionID
			if p.debug {
				log.Printf("[SESSION] Established session ID: %s", sessionID)
			}
		}
	}

	// Check for HTTP errors
	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Handle response based on content type
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		return p.handleSSEResponse(resp.Body)
	}

	return p.handleJSONResponse(resp.Body)
}

// handleJSONResponse handles a standard JSON response
func (p *Proxy) handleJSONResponse(body io.Reader) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// Validate it's valid JSON
	var msg JSONRPCMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("invalid JSON response: %w", err)
	}

	// Write to stdout
	fmt.Fprintf(p.stdout, "%s\n", data)
	if p.debug {
		log.Printf("[STDOUT] Sent JSON: %s", data)
	}

	return nil
}

// handleSSEResponse handles a Server-Sent Events stream
func (p *Proxy) handleSSEResponse(body io.Reader) error {
	scanner := bufio.NewScanner(body)
	// Increase buffer size to handle large SSE messages (default is 64KB)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()

		// SSE format: "data: {...}" or empty line (event boundary)
		if line == "" {
			// End of event, process accumulated data
			if len(dataLines) > 0 {
				jsonData := strings.Join(dataLines, "\n")
				if err := p.writeSSEData(jsonData); err != nil {
					log.Printf("[ERROR] Failed to write SSE data: %v", err)
				}
				dataLines = nil
			}
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			// Extract JSON data after "data: " prefix
			data := strings.TrimPrefix(line, "data: ")
			dataLines = append(dataLines, data)
		} else if strings.HasPrefix(line, ":") {
			// Comment line, ignore
			if p.debug {
				log.Printf("[SSE] Comment: %s", line)
			}
		} else if strings.HasPrefix(line, "event: ") {
			// Event type, ignore for now
			if p.debug {
				log.Printf("[SSE] Event type: %s", strings.TrimPrefix(line, "event: "))
			}
		}
	}

	// Process any remaining data
	if len(dataLines) > 0 {
		jsonData := strings.Join(dataLines, "\n")
		if err := p.writeSSEData(jsonData); err != nil {
			log.Printf("[ERROR] Failed to write final SSE data: %v", err)
		}
	}

	return scanner.Err()
}

// writeSSEData writes SSE data to stdout
func (p *Proxy) writeSSEData(data string) error {
	// Validate it's valid JSON
	var msg JSONRPCMessage
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		return fmt.Errorf("invalid JSON in SSE data: %w", err)
	}

	// Write to stdout
	fmt.Fprintf(p.stdout, "%s\n", data)
	if p.debug {
		log.Printf("[STDOUT] Sent SSE data: %s", data)
	}

	return nil
}

// sendErrorResponse sends a JSON-RPC error response to stdout
func (p *Proxy) sendErrorResponse(id json.RawMessage, code int, message string) {
	errResp := JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
		},
	}

	data, err := json.Marshal(errResp)
	if err != nil {
		log.Printf("[ERROR] Failed to marshal error response: %v", err)
		return
	}

	fmt.Fprintf(p.stdout, "%s\n", data)
	if p.debug {
		log.Printf("[STDOUT] Sent error: %s", data)
	}
}

// McpHubInstance represents a discovered mcp-hub process
type McpHubInstance struct {
	Port        string
	ConfigFiles []string
	CommandLine string
	PID         string
	ConfigPath  string // Primary config path for display
}

// discoverMcpHubInstance attempts to find the mcp-hub instance with full details
func discoverMcpHubInstance(debug bool) (*McpHubInstance, error) {
	if debug {
		log.SetOutput(os.Stderr)
		// Print current working directory
		cwd, err := os.Getwd()
		if err != nil {
			log.Printf("[DISCOVERY] Warning: Could not get current working directory: %v", err)
		} else {
			log.Printf("[DISCOVERY] Current working directory: %s", cwd)
		}
		log.Printf("[DISCOVERY] Attempting to discover mcp-hub port...")
	}

	// Strategy 1: Try to find mcp-hub in process list with --port argument
	instances, err := findAllMcpHubInstances(debug)
	if err == nil && len(instances) > 0 {
		if debug {
			log.Printf("[DISCOVERY] Found %d mcp-hub instance(s):", len(instances))
			for i, inst := range instances {
				log.Printf("[DISCOVERY] Instance %d:", i+1)
				log.Printf("[DISCOVERY]   PID: %s", inst.PID)
				log.Printf("[DISCOVERY]   Port: %s", inst.Port)
				log.Printf("[DISCOVERY]   Config files: %v", inst.ConfigFiles)
				log.Printf("[DISCOVERY]   Command: %s", inst.CommandLine)
			}
		}

		// Select best instance based on project-local configs
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "" // Fall back to first instance if we can't get CWD
		}
		selected := selectBestMcpHubInstance(instances, cwd, debug)

		// Set primary config path for display
		if len(selected.ConfigFiles) > 0 {
			// Use the last (most specific) config file
			selected.ConfigPath = selected.ConfigFiles[len(selected.ConfigFiles)-1]

			// Replace $HOME with ~/ for shorter ps output
			if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
				selected.ConfigPath = strings.Replace(selected.ConfigPath, homeDir, "~", 1)
			}
		}

		return selected, nil
	}
	if debug {
		log.Printf("[DISCOVERY] Process list search failed: %v", err)
	}

	// Strategy 2: Try to find listening port using ss/netstat (fallback, no config info)
	port, err := findPortInNetstat(debug)
	if err == nil {
		return &McpHubInstance{
			Port:       port,
			ConfigPath: "unknown",
		}, nil
	}
	if debug {
		log.Printf("[DISCOVERY] Network socket search failed: %v", err)
	}

	return nil, fmt.Errorf("could not discover mcp-hub port")
}

// discoverMcpHubPort attempts to find the port mcp-hub is running on (legacy function)
func discoverMcpHubPort(debug bool) (string, error) {
	instance, err := discoverMcpHubInstance(debug)
	if err != nil {
		return "", err
	}
	return instance.Port, nil
}

// findAllMcpHubInstances searches for all mcp-hub processes and returns their details
func findAllMcpHubInstances(debug bool) ([]McpHubInstance, error) {
	// Use ps to find mcp-hub processes with full command line
	cmd := exec.Command("ps", "auxww")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run ps: %w", err)
	}

	var instances []McpHubInstance
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	portRegex := regexp.MustCompile(`--port[= ](\d+)`)
	configRegex := regexp.MustCompile(`--config\s+([^\s]+)`)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "mcp-hub") {
			continue
		}
		// Skip grep processes
		if strings.Contains(line, "grep") {
			continue
		}

		// Extract PID (second field in ps aux output)
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid := fields[1]

		// Extract port from --port argument
		portMatches := portRegex.FindStringSubmatch(line)
		if len(portMatches) < 2 {
			continue
		}
		port := portMatches[1]

		// Extract all --config arguments
		var configFiles []string
		configMatches := configRegex.FindAllStringSubmatch(line, -1)
		for _, match := range configMatches {
			if len(match) >= 2 {
				configFiles = append(configFiles, match[1])
			}
		}

		instances = append(instances, McpHubInstance{
			Port:        port,
			ConfigFiles: configFiles,
			CommandLine: line,
			PID:         pid,
		})
	}

	if len(instances) == 0 {
		return nil, fmt.Errorf("no mcp-hub processes found in process list")
	}

	return instances, nil
}

// findPortInNetstat searches for mcp-hub listening port using ss or netstat
func findPortInNetstat(debug bool) (string, error) {
	// Try ss first (modern Linux)
	port, err := tryNetworkCommand("ss", []string{"-tlnp"}, debug)
	if err == nil {
		return port, nil
	}

	// Fall back to netstat
	port, err = tryNetworkCommand("netstat", []string{"-tlnp"}, debug)
	if err == nil {
		return port, nil
	}

	return "", fmt.Errorf("could not find mcp-hub listening port")
}

// tryNetworkCommand tries to run a network command (ss or netstat) and find mcp-hub
func tryNetworkCommand(command string, args []string, debug bool) (string, error) {
	cmd := exec.Command(command, args...)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to run %s: %w", command, err)
	}

	// Look for lines containing "node" or "mcp-hub" that are LISTEN
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	portRegex := regexp.MustCompile(`:(\d+)\s`)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "LISTEN") {
			continue
		}
		if !strings.Contains(line, "node") && !strings.Contains(line, "mcp-hub") {
			continue
		}

		// Extract port from address (format: 0.0.0.0:PORT or :::PORT)
		matches := portRegex.FindStringSubmatch(line)
		if len(matches) >= 2 {
			port := matches[1]
			if debug {
				log.Printf("[DISCOVERY] Found potential mcp-hub port in %s: %s", command, line)
				log.Printf("[DISCOVERY] Extracted port: %s", port)
			}
			return port, nil
		}
	}

	return "", fmt.Errorf("no matching process found in %s output", command)
}

// selectBestMcpHubInstance chooses the best mcp-hub instance based on project-local configs
func selectBestMcpHubInstance(instances []McpHubInstance, cwd string, debug bool) *McpHubInstance {
	if len(instances) == 0 {
		return nil
	}

	// If only one instance, return it
	if len(instances) == 1 {
		if debug {
			log.Printf("[DISCOVERY] Only one instance found, selecting port %s", instances[0].Port)
		}
		return &instances[0]
	}

	// Score each instance
	type scoredInstance struct {
		instance *McpHubInstance
		score    int
		reason   string
	}

	var scored []scoredInstance

	for i := range instances {
		inst := &instances[i]
		score, reason := scoreInstance(inst, cwd, debug)
		scored = append(scored, scoredInstance{
			instance: inst,
			score:    score,
			reason:   reason,
		})
	}

	// Sort by score (highest first)
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	if debug {
		log.Printf("[DISCOVERY] Instance scoring:")
		for i, s := range scored {
			log.Printf("[DISCOVERY]   Instance %d (port %s): score=%d - %s",
				i+1, s.instance.Port, s.score, s.reason)
		}
		log.Printf("[DISCOVERY] Selected instance with port %s", scored[0].instance.Port)
	}

	return scored[0].instance
}

// scoreInstance calculates a priority score for an mcp-hub instance
func scoreInstance(inst *McpHubInstance, cwd string, debug bool) (int, string) {
	if cwd == "" {
		return 0, "no CWD available, using default priority"
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = ""
	}

	var globalConfigPath string
	if homeDir != "" {
		globalConfigPath = filepath.Join(homeDir, ".mcp-hub")
	}

	maxScore := 0
	bestReason := "global config only"

	for _, configPath := range inst.ConfigFiles {
		// Skip global configs
		if globalConfigPath != "" && strings.HasPrefix(configPath, globalConfigPath) {
			continue
		}

		// Get the directory of the config file
		configDir := filepath.Dir(configPath)

		// Calculate how closely related the config is to CWD
		commonLength := commonPathLength(cwd, configDir)

		// Award points: more common path components = higher score
		score := commonLength * 100

		// Bonus points if config is in a parent directory (typical project structure)
		if strings.HasPrefix(cwd, configDir) {
			score += 50
		}

		// Bonus points if config is in a child directory
		if strings.HasPrefix(configDir, cwd) {
			score += 25
		}

		if score > maxScore {
			maxScore = score
			bestReason = fmt.Sprintf("project-local config at %s (common path length: %d)", configPath, commonLength)
		}
	}

	return maxScore, bestReason
}

// commonPathLength calculates the number of common path components between two paths
func commonPathLength(path1, path2 string) int {
	// Clean and split paths
	p1 := filepath.Clean(path1)
	p2 := filepath.Clean(path2)

	parts1 := strings.Split(p1, string(filepath.Separator))
	parts2 := strings.Split(p2, string(filepath.Separator))

	// Count common prefix parts
	common := 0
	for i := 0; i < len(parts1) && i < len(parts2); i++ {
		if parts1[i] == parts2[i] {
			common++
		} else {
			break
		}
	}

	return common
}
