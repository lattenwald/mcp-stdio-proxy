.PHONY: all build install test lint fmt vet clean help

# Binary name
BINARY_NAME=mcp-stdio-proxy

# Installation directories
INSTALL_DIR_USER=$(HOME)/bin
INSTALL_DIR_ROOT=/usr/local/bin

# Determine install directory based on user
ifeq ($(shell id -u),0)
    INSTALL_DIR=$(INSTALL_DIR_ROOT)
else
    INSTALL_DIR=$(INSTALL_DIR_USER)
endif

# Default target
all: build install

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	go build -o $(BINARY_NAME)
	@echo "Build complete: $(BINARY_NAME)"

# Install binary to appropriate location
install: build
	@echo "Installing $(BINARY_NAME) to $(INSTALL_DIR)..."
	@mkdir -p $(INSTALL_DIR)
	@cp $(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)
	@chmod +x $(INSTALL_DIR)/$(BINARY_NAME)
	@echo "Installed to $(INSTALL_DIR)/$(BINARY_NAME)"

# Run tests (if any exist)
test:
	@echo "Running tests..."
	@if ls *_test.go >/dev/null 2>&1; then \
		go test -v ./...; \
	else \
		echo "No tests found"; \
	fi

# Run all linters
lint: fmt vet
	@echo "Running golangci-lint..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed, skipping..."; \
		echo "Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Run go vet
vet:
	@echo "Running go vet..."
	go vet ./...

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -f $(BINARY_NAME)
	@echo "Clean complete"

# Show help
help:
	@echo "Available targets:"
	@echo "  all        - Build and install (default)"
	@echo "  build      - Build the binary"
	@echo "  install    - Install binary (~/bin for user, /usr/local/bin for root)"
	@echo "  test       - Run tests"
	@echo "  lint       - Run all linters (fmt, vet, golangci-lint)"
	@echo "  fmt        - Format code with go fmt"
	@echo "  vet        - Run go vet"
	@echo "  clean      - Remove build artifacts"
	@echo "  help       - Show this help message"
	@echo ""
	@echo "Current install directory: $(INSTALL_DIR)"
