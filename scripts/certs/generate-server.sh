#!/bin/bash
# Generate server certificate for Marionette gRPC server
# Signed by the CA certificate

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUTPUT_DIR="${OUTPUT_DIR:-${SCRIPT_DIR}/../../certs}"
CONFIG_FILE="${SCRIPT_DIR}/openssl-server.cnf"

# Server configuration
SERVER_DAYS="${SERVER_DAYS:-365}"  # 1 year
SERVER_KEY_SIZE="${SERVER_KEY_SIZE:-2048}"
SERVER_CN="${SERVER_CN:-marionette-server}"
SERVER_ORG="${SERVER_ORG:-Marionette}"
SERVER_COUNTRY="${SERVER_COUNTRY:-US}"

# SAN (Subject Alternative Names)
# Comma-separated list of DNS names and IP addresses
SERVER_DNS="${SERVER_DNS:-localhost,marionette-server}"
SERVER_IP="${SERVER_IP:-127.0.0.1,::1}"

echo "Generating server certificate..."
echo "  Output directory: ${OUTPUT_DIR}"
echo "  Validity: ${SERVER_DAYS} days"
echo "  Key size: ${SERVER_KEY_SIZE} bits"
echo "  Common Name: ${SERVER_CN}"
echo "  DNS names: ${SERVER_DNS}"
echo "  IP addresses: ${SERVER_IP}"

# Check CA exists
if [ ! -f "${OUTPUT_DIR}/ca.key" ] || [ ! -f "${OUTPUT_DIR}/ca.crt" ]; then
    echo "Error: CA certificate not found. Run generate-ca.sh first."
    exit 1
fi

# Build SAN extension
build_san() {
    local san=""
    local count=1

    # Add DNS names
    IFS=',' read -ra DNS_ARRAY <<< "${SERVER_DNS}"
    for dns in "${DNS_ARRAY[@]}"; do
        if [ -n "${san}" ]; then
            san="${san},"
        fi
        san="${san}DNS.${count}:${dns}"
        ((count++))
    done

    # Add IP addresses
    count=1
    IFS=',' read -ra IP_ARRAY <<< "${SERVER_IP}"
    for ip in "${IP_ARRAY[@]}"; do
        if [ -n "${san}" ]; then
            san="${san},"
        fi
        san="${san}IP.${count}:${ip}"
        ((count++))
    done

    echo "${san}"
}

SAN=$(build_san)

# Create OpenSSL config for SAN
cat > "${CONFIG_FILE}" <<EOF
[req]
distinguished_name = req_dn
req_extensions = v3_req
prompt = no

[req_dn]
C = ${SERVER_COUNTRY}
O = ${SERVER_ORG}
CN = ${SERVER_CN}

[v3_req]
basicConstraints = CA:FALSE
keyUsage = critical, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = ${SAN}

[v3_ca]
basicConstraints = CA:FALSE
keyUsage = critical, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = ${SAN}
EOF

# Generate server private key
echo "Generating server private key..."
openssl genrsa -out "${OUTPUT_DIR}/server.key" "${SERVER_KEY_SIZE}"
chmod 600 "${OUTPUT_DIR}/server.key"

# Generate CSR
echo "Generating certificate signing request..."
openssl req -new \
    -key "${OUTPUT_DIR}/server.key" \
    -out "${OUTPUT_DIR}/server.csr" \
    -config "${CONFIG_FILE}"

# Sign with CA
echo "Signing certificate with CA..."
openssl x509 -req \
    -in "${OUTPUT_DIR}/server.csr" \
    -CA "${OUTPUT_DIR}/ca.crt" \
    -CAkey "${OUTPUT_DIR}/ca.key" \
    -CAcreateserial \
    -out "${OUTPUT_DIR}/server.crt" \
    -days "${SERVER_DAYS}" \
    -extfile "${CONFIG_FILE}" \
    -extensions v3_ca

# Clean up CSR and config
rm -f "${OUTPUT_DIR}/server.csr" "${CONFIG_FILE}"

echo "Server certificate generated successfully:"
echo "  Private key: ${OUTPUT_DIR}/server.key"
echo "  Certificate: ${OUTPUT_DIR}/server.crt"
echo ""
echo "Certificate info:"
openssl x509 -in "${OUTPUT_DIR}/server.crt" -noout -subject -issuer -dates
echo ""
echo "SAN entries:"
openssl x509 -in "${OUTPUT_DIR}/server.crt" -noout -ext subjectAltName
