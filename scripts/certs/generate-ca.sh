#!/bin/bash
# Generate CA (Certificate Authority) certificate
# This CA is used to sign both server and client certificates for mTLS

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUTPUT_DIR="${OUTPUT_DIR:-${SCRIPT_DIR}/../../certs}"

# CA configuration
CA_DAYS="${CA_DAYS:-3650}"  # 10 years
CA_KEY_SIZE="${CA_KEY_SIZE:-4096}"
CA_CN="${CA_CN:-Marionette CA}"
CA_ORG="${CA_ORG:-Marionette}"
CA_COUNTRY="${CA_COUNTRY:-US}"

echo "Generating CA certificate..."
echo "  Output directory: ${OUTPUT_DIR}"
echo "  Validity: ${CA_DAYS} days"
echo "  Key size: ${CA_KEY_SIZE} bits"

# Create output directory
mkdir -p "${OUTPUT_DIR}"

# Generate CA private key
echo "Generating CA private key..."
openssl genrsa -out "${OUTPUT_DIR}/ca.key" "${CA_KEY_SIZE}"
chmod 600 "${OUTPUT_DIR}/ca.key"

# Generate CA certificate
echo "Generating CA certificate..."
openssl req -new -x509 \
    -key "${OUTPUT_DIR}/ca.key" \
    -out "${OUTPUT_DIR}/ca.crt" \
    -days "${CA_DAYS}" \
    -subj "/C=${CA_COUNTRY}/O=${CA_ORG}/CN=${CA_CN}" \
    -addext "basicConstraints=critical,CA:TRUE" \
    -addext "keyUsage=critical,keyCertSign,cRLSign"

echo "CA certificate generated successfully:"
echo "  Private key: ${OUTPUT_DIR}/ca.key"
echo "  Certificate: ${OUTPUT_DIR}/ca.crt"
echo ""
echo "Certificate info:"
openssl x509 -in "${OUTPUT_DIR}/ca.crt" -noout -subject -issuer -dates
