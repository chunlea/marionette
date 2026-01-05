# Certificate Generation Makefile
# Usage:
#   make all       - Generate CA, server, and client certificates
#   make ca        - Generate CA certificate only
#   make server    - Generate server certificate (requires CA)
#   make client    - Generate client certificate (requires CA)
#   make verify    - Verify all certificates
#   make clean     - Remove all generated certificates

SHELL := /bin/bash
SCRIPT_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
OUTPUT_DIR ?= $(SCRIPT_DIR)../../certs

# Export for scripts
export OUTPUT_DIR

.PHONY: all ca server client verify clean help

help:
	@echo "Certificate Generation Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make all       - Generate CA, server, and client certificates"
	@echo "  make ca        - Generate CA certificate only"
	@echo "  make server    - Generate server certificate (requires CA)"
	@echo "  make client    - Generate client certificate (requires CA)"
	@echo "  make verify    - Verify all certificates against CA"
	@echo "  make clean     - Remove all generated certificates"
	@echo ""
	@echo "Environment variables:"
	@echo "  OUTPUT_DIR     - Output directory (default: ../../certs)"
	@echo "  CA_DAYS        - CA validity in days (default: 3650)"
	@echo "  SERVER_DAYS    - Server cert validity in days (default: 365)"
	@echo "  CLIENT_DAYS    - Client cert validity in days (default: 365)"
	@echo "  SERVER_DNS     - Server DNS SANs, comma-separated (default: localhost,marionette-server)"
	@echo "  SERVER_IP      - Server IP SANs, comma-separated (default: 127.0.0.1,::1)"
	@echo "  CLIENT_NAME    - Client cert name prefix (default: client)"
	@echo "  CLIENT_CN      - Client common name (default: marionette-agent)"

all: ca server client
	@echo ""
	@echo "All certificates generated successfully!"
	@echo "Files are in: $(OUTPUT_DIR)"

ca:
	@echo "=== Generating CA certificate ==="
	@bash $(SCRIPT_DIR)/generate-ca.sh
	@echo ""

server: $(OUTPUT_DIR)/ca.crt
	@echo "=== Generating server certificate ==="
	@bash $(SCRIPT_DIR)/generate-server.sh
	@echo ""

client: $(OUTPUT_DIR)/ca.crt
	@echo "=== Generating client certificate ==="
	@bash $(SCRIPT_DIR)/generate-client.sh
	@echo ""

$(OUTPUT_DIR)/ca.crt:
	@echo "CA certificate not found. Generating..."
	@$(MAKE) ca

verify:
	@echo "=== Verifying certificates ==="
	@echo ""
	@echo "Verifying CA certificate..."
	@openssl x509 -in $(OUTPUT_DIR)/ca.crt -noout -text | head -20
	@echo ""
	@if [ -f "$(OUTPUT_DIR)/server.crt" ]; then \
		echo "Verifying server certificate against CA..."; \
		openssl verify -CAfile $(OUTPUT_DIR)/ca.crt $(OUTPUT_DIR)/server.crt; \
	else \
		echo "Server certificate not found, skipping..."; \
	fi
	@echo ""
	@if [ -f "$(OUTPUT_DIR)/client.crt" ]; then \
		echo "Verifying client certificate against CA..."; \
		openssl verify -CAfile $(OUTPUT_DIR)/ca.crt $(OUTPUT_DIR)/client.crt; \
	else \
		echo "Client certificate not found, skipping..."; \
	fi
	@echo ""
	@echo "Verification complete!"

clean:
	@echo "Removing all certificates from $(OUTPUT_DIR)..."
	@rm -rf $(OUTPUT_DIR)
	@echo "Done!"
