#!/bin/bash
# ==============================================================================
# LegacyTel TLS & mTLS Certificate Generator Utility
# ==============================================================================
# This script generates a self-signed Root Certificate Authority (CA) and 
# separate, highly secure server and client certificates. These are used to 
# configure standard TLS and Mutual TLS (mTLS) for LegacyTel log streams.
# ==============================================================================

set -e

# Configuration
CERT_DIR="./certs"
DAYS_VALID=3650
KEY_SIZE=4096

echo "=== Creating certificate directory: ${CERT_DIR} ==="
mkdir -p "${CERT_DIR}"
cd "${CERT_DIR}"

# ------------------------------------------------------------------------------
# 1. Generate Root CA (Certificate Authority)
# ------------------------------------------------------------------------------
echo ""
echo "=== 1. Generating Root CA (Authority that validates all nodes) ==="
openssl req -x509 -new -newkey rsa:${KEY_SIZE} -nodes \
  -keyout root_ca.key \
  -out root_ca.crt \
  -days ${DAYS_VALID} \
  -subj "/CN=LegacyTel-Root-CA/O=Enterprise/OU=CyberSecurity"

# ------------------------------------------------------------------------------
# 2. Generate Collector Server Certificates (The LegacyTel Gateway)
# ------------------------------------------------------------------------------
echo ""
echo "=== 2. Generating LegacyTel Collector Gateway Certificates ==="
# Server Private Key
openssl genrsa -out server.key ${KEY_SIZE}

# Server Certificate Signing Request (CSR)
openssl req -new -key server.key \
  -out server.csr \
  -subj "/CN=legacytel-collector.local/O=Enterprise/OU=Security-Operations"

# Create SAN (Subject Alternative Names) file for hostname/IP resolution
cat <<EOF > server_ext.cnf
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, nonRepudiation, keyEncipherment, dataEncipherment
subjectAltName = @alt_names

[alt_names]
DNS.1 = localhost
DNS.2 = legacytel-collector.local
IP.1 = 127.0.0.1
EOF

# Sign Server Certificate using Root CA
openssl x509 -req -in server.csr \
  -CA root_ca.crt -CAkey root_ca.key -CAcreateserial \
  -out server.crt \
  -days ${DAYS_VALID} -sha256 \
  -extfile server_ext.cnf

# ------------------------------------------------------------------------------
# 3. Generate Client Certificates for Legacy Source Systems
# ------------------------------------------------------------------------------
echo ""
echo "=== 3. Generating Mainframe & Legacy Client Node Certificates ==="

# Systems list
systems=("zos_mainframe" "as400_iseries" "tandem_nonstop")

for sys in "${systems[@]}"; do
  echo ""
  echo "--> Generating credentials for client node: ${sys}"
  # Client Private Key
  openssl genrsa -out "${sys}_client.key" ${KEY_SIZE}
  
  # Client CSR
  openssl req -new -key "${sys}_client.key" \
    -out "${sys}_client.csr" \
    -subj "/CN=${sys}-node/O=Enterprise/OU=Legacy-Hosts"
    
  # Create client extensions file
  cat <<EOF > "${sys}_ext.cnf"
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth
EOF

  # Sign Client Certificate using Root CA
  openssl x509 -req -in "${sys}_client.csr" \
    -CA root_ca.crt -CAkey root_ca.key -CAcreateserial \
    -out "${sys}_client.crt" \
    -days ${DAYS_VALID} -sha256 \
    -extfile "${sys}_ext.cnf"
    
  # Clean up CSR/extensions files
  rm "${sys}_client.csr" "${sys}_ext.cnf"
done

# Clean up server temp files
rm server.csr server_ext.cnf root_ca.srl

echo ""
echo "=============================================================================="
echo "=== CERTIFICATE GENERATION COMPLETE! ==="
echo "=============================================================================="
echo "Files located in: $(pwd)"
echo "------------------------------------------------------------------------------"
echo "1. LegacyTel Collector Gateway Files (Install on collector side):"
echo "   - root_ca.crt (Loaded as client_ca_file for mTLS client validation)"
echo "   - server.crt  (Loaded as cert_file)"
echo "   - server.key  (Loaded as key_file)"
echo ""
echo "2. Legacy System Node Files (Install on mainframe/source systems):"
echo "   - zos_mainframe_client.crt  /  zos_mainframe_client.key"
echo "   - as400_iseries_client.crt   /  as400_iseries_client.key"
echo "   - tandem_nonstop_client.crt  /  tandem_nonstop_client.key"
echo "   - root_ca.crt (Loaded on source clients to verify Collector identity)"
echo "=============================================================================="
