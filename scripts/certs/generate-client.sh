#!/bin/bash
# Generate client certificate for Marionette agent (mTLS)
# Signed by the CA certificate

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUTPUT_DIR="${OUTPUT_DIR:-${SCRIPT_DIR}/../../certs}"
CONFIG_FILE="${SCRIPT_DIR}/openssl-client.cnf"

# Client configuration
CLIENT_DAYS="${CLIENT_DAYS:-365}"  # 1 year
CLIENT_KEY_SIZE="${CLIENT_KEY_SIZE:-2048}"
CLIENT_CN="${CLIENT_CN:-marionette-agent}"
CLIENT_ORG="${CLIENT_ORG:-Marionette}"
CLIENT_COUNTRY="${CLIENT_COUNTRY:-US}"

# Optional: Client name suffix for generating multiple client certs
CLIENT_NAME="${CLIENT_NAME:-client}"

echo "Generating client certificate..."
echo "  Output directory: ${OUTPUT_DIR}"
echo "  Validity: ${CLIENT_DAYS} days"
echo "  Key size: ${CLIENT_KEY_SIZE} bits"
echo "  Common Name: ${CLIENT_CN}"
echo "  Output name: ${CLIENT_NAME}"

# Check CA exists
if [ ! -f "${OUTPUT_DIR}/ca.key" ] || [ ! -f "${OUTPUT_DIR}/ca.crt" ]; then
    echo "Error: CA certificate not found. Run generate-ca.sh first."
    exit 1
fi

# Create OpenSSL config
cat > "${CONFIG_FILE}" <<EOF
[req]
distinguished_name = req_dn
req_extensions = v3_req
prompt = no

[req_dn]
C = ${CLIENT_COUNTRY}
O = ${CLIENT_ORG}
CN = ${CLIENT_CN}

[v3_req]
basicConstraints = CA:FALSE
keyUsage = critical, digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth

[v3_ca]
basicConstraints = CA:FALSE
keyUsage = critical, digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth
EOF

# Generate client private key
echo "Generating client private key..."
openssl genrsa -out "${OUTPUT_DIR}/${CLIENT_NAME}.key" "${CLIENT_KEY_SIZE}"
chmod 600 "${OUTPUT_DIR}/${CLIENT_NAME}.key"

# Generate CSR
echo "Generating certificate signing request..."
openssl req -new \
    -key "${OUTPUT_DIR}/${CLIENT_NAME}.key" \
    -out "${OUTPUT_DIR}/${CLIENT_NAME}.csr" \
    -config "${CONFIG_FILE}"

# Sign with CA
echo "Signing certificate with CA..."
openssl x509 -req \
    -in "${OUTPUT_DIR}/${CLIENT_NAME}.csr" \
    -CA "${OUTPUT_DIR}/ca.crt" \
    -CAkey "${OUTPUT_DIR}/ca.key" \
    -CAcreateserial \
    -out "${OUTPUT_DIR}/${CLIENT_NAME}.crt" \
    -days "${CLIENT_DAYS}" \
    -extfile "${CONFIG_FILE}" \
    -extensions v3_ca

# Clean up CSR and config
rm -f "${OUTPUT_DIR}/${CLIENT_NAME}.csr" "${CONFIG_FILE}"

echo "Client certificate generated successfully:"
echo "  Private key: ${OUTPUT_DIR}/${CLIENT_NAME}.key"
echo "  Certificate: ${OUTPUT_DIR}/${CLIENT_NAME}.crt"
echo ""
echo "Certificate info:"
openssl x509 -in "${OUTPUT_DIR}/${CLIENT_NAME}.crt" -noout -subject -issuer -dates
