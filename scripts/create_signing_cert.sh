#!/bin/bash
# create_signing_cert.sh: Create a persistent self-signed code-signing certificate
# named "Clara Development" in the login keychain.
#
# Using a stable certificate identity instead of ad-hoc signing (--sign -)
# means TCC grants (Full Disk Access, Reminders, Calendar, etc.) survive rebuilds,
# because macOS associates them with the certificate identity rather than the binary hash.
#
# Run once: make sign-cert
# Then use make install as usual; SIGN_IDENTITY is auto-detected by the Makefile.

set -e

CERT_NAME="Clara Development"
KEYCHAIN="$HOME/Library/Keychains/login.keychain-db"

# Idempotent: skip if the certificate already exists.
if security find-identity -v -p codesigning 2>/dev/null | grep -q "\"$CERT_NAME\""; then
    echo "Certificate '$CERT_NAME' already exists in the login keychain — nothing to do."
    exit 0
fi

echo "Creating self-signed code-signing certificate '$CERT_NAME'..."

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

# OpenSSL config: RSA-2048, code-signing EKU only.
cat > "$TMPDIR/cert.cfg" << 'EOF'
[ req ]
default_bits       = 2048
distinguished_name = req_dn
x509_extensions    = v3_cs
prompt             = no

[ req_dn ]
CN = Clara Development

[ v3_cs ]
keyUsage             = critical, digitalSignature
extendedKeyUsage     = critical, codeSigning
subjectKeyIdentifier = hash
EOF

openssl req -x509 -newkey rsa:2048 \
    -keyout "$TMPDIR/key.pem" \
    -out    "$TMPDIR/cert.pem" \
    -days 3650 -nodes \
    -config "$TMPDIR/cert.cfg" \
    2>/dev/null

# Import key and cert as separate PEM items; macOS Keychain automatically links
# a private key with a certificate that holds its matching public key, forming a
# digital identity without needing PKCS12 (which has OpenSSL 3.x compat issues).
security import "$TMPDIR/key.pem" \
    -k "$KEYCHAIN" \
    -A \
    -T /usr/bin/codesign

security import "$TMPDIR/cert.pem" \
    -k "$KEYCHAIN" \
    -A \
    -T /usr/bin/codesign

# Trust the cert for code signing in the login keychain.
# macOS will prompt for your login password to authorise the trust change.
security add-trusted-cert \
    -r trustRoot \
    -k "$KEYCHAIN" \
    "$TMPDIR/cert.pem"

echo ""
echo "Certificate '$CERT_NAME' created and trusted for code signing."
echo "Run 'make install' to rebuild clara and ClaraBridge with the persistent identity."
echo "TCC permissions (Full Disk Access, Reminders, Calendar, etc.) will now survive rebuilds."
