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
	"strings"
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

	// Custom usage message
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS] <streamable-http-url>\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "A minimal stdio to Streamable HTTP proxy for Model Context Protocol (MCP).\n\n")
		fmt.Fprintf(os.Stderr, "Arguments:\n")
		fmt.Fprintf(os.Stderr, "  <streamable-http-url>  Target MCP server URL (required)\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s http://localhost:37373/mcp\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --debug http://localhost:37373/mcp\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -v http://localhost:37373/mcp\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nEnvironment Variables:\n")
		fmt.Fprintf(os.Stderr, "  DEBUG=1  Alternative way to enable debug logging\n")
	}

	// Parse flags
	flag.Parse()

	// Get URL from remaining arguments
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}

	url := flag.Arg(0)

	// Validate URL
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		fmt.Fprintf(os.Stderr, "Error: URL must start with http:// or https://\n")
		os.Exit(1)
	}

	// Check for debug mode (flag or environment variable)
	debug := *debugFlag || *verboseFlag || os.Getenv("DEBUG") == "1"

	// Create proxy
	stdinScanner := bufio.NewScanner(os.Stdin)
	// Increase buffer size to handle large JSON-RPC messages (default is 64KB)
	// 1MB should handle even very large tool lists and resource contents
	stdinScanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	proxy := &Proxy{
		url: url,
		client: &http.Client{
			Timeout: 60 * time.Second,
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
