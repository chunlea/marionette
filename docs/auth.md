# Authentication & Token Design

## Overview

Marionette uses token-based authentication for all components. All tokens are randomly generated with high entropy (256 bits), making brute-force attacks infeasible.

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Token Types                                 │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Token Type        Prefix    Purpose                    Lifetime    │
│  ─────────────────────────────────────────────────────────────────  │
│  API Key           mk_       CLI / External API access  Long-term   │
│  Runner Token      rtok_     Runner authentication      Long-term   │
│  Tunnel Token      ttok_     Tunnel access              Short-term  │
│                                                                     │
│  Agent API Key     (varies)  AI provider credentials    Long-term   │
│                              (encrypted, not hashed)                │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

## Token Format

All tokens follow a consistent format:

```
{prefix}_{base64url_random_bytes}

Examples:
  mk_dGhpcyBpcyBhIHRlc3QgdG9rZW4gZm9yIGRlbW8
  rtok_YW5vdGhlciB0ZXN0IHRva2VuIGZvciBkZW1v
  ttok_eWV0IGFub3RoZXIgdGVzdCB0b2tlbiBmb3I
      │ └───────────────────────────────────┘
      │              base64url (32 bytes)
      └── prefix (identifies token type)
```

### Token Components

| Component | Size | Description |
|-----------|------|-------------|
| Prefix | 2-5 chars | Token type identifier |
| Separator | 1 char | Underscore `_` |
| Random | 32 bytes | Cryptographically random, base64url encoded |

**Total length**: ~48 characters

## Storage: SHA-256 Hash

Tokens are stored as SHA-256 hashes. For 256-bit random tokens, SHA-256 provides sufficient security without the performance overhead of slow hashes like Argon2.

### Why SHA-256 (not Argon2)?

```
┌─────────────────────────────────────────────────────────────────────┐
│                     Hash Algorithm Selection                        │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  User Passwords (low entropy, ~40 bits):                            │
│    → Argon2/bcrypt required (slow hash prevents brute-force)        │
│    → Verification: ~50-100ms                                        │
│                                                                     │
│  Random Tokens (high entropy, 256 bits):                            │
│    → SHA-256 sufficient (brute-force infeasible)                    │
│    → Verification: ~1μs                                             │
│                                                                     │
│  Brute-force 256-bit token:                                         │
│    2^256 attempts ≈ 10^77 operations                                │
│    At 1 trillion/sec: ~10^58 years                                  │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### Hash Version Support

To support future algorithm upgrades (e.g., HMAC-SHA256), all token tables include a `hash_version` field:

| Version | Algorithm | Status |
|---------|-----------|--------|
| 1 | SHA-256 | Current |
| 2 | HMAC-SHA256 | Reserved |

## Implementation

### Token Generation

```go
// pkg/crypto/token.go

package crypto

import (
    "crypto/rand"
    "crypto/sha256"
    "crypto/subtle"
    "encoding/base64"
    "encoding/hex"
)

const (
    HashV1_SHA256      = 1  // Current
    HashV2_HMAC_SHA256 = 2  // Reserved for future
)

var CurrentHashVersion = HashV1_SHA256

// GenerateToken generates a new token with the given prefix
// Returns: (full_token, display_prefix, hash, version, error)
func GenerateToken(prefix string) (token, displayPrefix, hash string, version int, err error) {
    raw := make([]byte, 32) // 256 bits
    if _, err := rand.Read(raw); err != nil {
        return "", "", "", 0, err
    }

    encoded := base64.RawURLEncoding.EncodeToString(raw)
    token = prefix + encoded
    displayPrefix = prefix + encoded[:8]  // For display/logging
    hash = sha256Hex(token)

    return token, displayPrefix, hash, CurrentHashVersion, nil
}

func sha256Hex(s string) string {
    h := sha256.Sum256([]byte(s))
    return hex.EncodeToString(h[:])
}
```

### Token Verification

```go
// VerifyToken verifies a token against stored hash
// Supports multiple hash versions for migration
func VerifyToken(token, storedHash string, version int, hmacKey []byte) bool {
    var computed string

    switch version {
    case HashV1_SHA256:
        computed = sha256Hex(token)
    case HashV2_HMAC_SHA256:
        if hmacKey == nil {
            return false
        }
        mac := hmac.New(sha256.New, hmacKey)
        mac.Write([]byte(token))
        computed = hex.EncodeToString(mac.Sum(nil))
    default:
        return false
    }

    // Constant-time comparison to prevent timing attacks
    return subtle.ConstantTimeCompare([]byte(computed), []byte(storedHash)) == 1
}
```

### Convenience Functions

```go
// Token type prefixes
const (
    PrefixAPIKey      = "mk_"
    PrefixRunnerToken = "rtok_"
    PrefixTunnelToken = "ttok_"
)

func GenerateAPIKey() (token, prefix, hash string, version int, err error) {
    return GenerateToken(PrefixAPIKey)
}

func GenerateRunnerToken() (token, prefix, hash string, version int, err error) {
    return GenerateToken(PrefixRunnerToken)
}

func GenerateTunnelToken() (token, prefix, hash string, version int, err error) {
    return GenerateToken(PrefixTunnelToken)
}
```

## Database Schema

### API Keys

```sql
CREATE TABLE api_keys (
    id TEXT PRIMARY KEY,              -- key_xxx
    name TEXT NOT NULL,
    key_hash TEXT NOT NULL UNIQUE,    -- SHA-256 hex (64 chars)
    key_prefix TEXT NOT NULL,         -- mk_xxxxxxxx (for display)
    hash_version INT NOT NULL DEFAULT 1,
    scopes TEXT[] NOT NULL DEFAULT '{}',

    tenant_id TEXT,
    labels JSONB NOT NULL DEFAULT '{}',
    annotations JSONB NOT NULL DEFAULT '{}',

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by TEXT,
    last_used_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    revoke_reason TEXT
);

CREATE INDEX idx_api_keys_hash ON api_keys(key_hash);
CREATE INDEX idx_api_keys_prefix ON api_keys(key_prefix);
CREATE INDEX idx_api_keys_tenant ON api_keys(tenant_id);
```

### Runner Tokens

```sql
CREATE TABLE runner_tokens (
    id TEXT PRIMARY KEY,              -- rtok_xxx
    token_hash TEXT NOT NULL UNIQUE,
    token_prefix TEXT NOT NULL,
    hash_version INT NOT NULL DEFAULT 1,

    runner_id TEXT REFERENCES runners(id) ON DELETE CASCADE,
    pool_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',  -- active, revoked, expired

    -- Rotation support
    previous_token_hash TEXT,
    rotation_deadline TIMESTAMPTZ,

    tenant_id TEXT,
    labels JSONB NOT NULL DEFAULT '{}',

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by TEXT,
    last_used_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    revoke_reason TEXT,

    CONSTRAINT valid_token_status CHECK (
        status IN ('active', 'rotating', 'revoked', 'expired')
    )
);

CREATE INDEX idx_runner_tokens_hash ON runner_tokens(token_hash);
CREATE INDEX idx_runner_tokens_prefix ON runner_tokens(token_prefix);
CREATE INDEX idx_runner_tokens_pool ON runner_tokens(pool_name);
CREATE INDEX idx_runner_tokens_runner ON runner_tokens(runner_id);
```

### Tunnel Tokens

```sql
CREATE TABLE tunnel_tokens (
    id TEXT PRIMARY KEY,              -- ttok_xxx
    token_hash TEXT NOT NULL UNIQUE,
    token_prefix TEXT NOT NULL,
    hash_version INT NOT NULL DEFAULT 1,

    tunnel_id TEXT NOT NULL REFERENCES tunnels(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL,

    tenant_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,  -- Required for tunnel tokens
    revoked_at TIMESTAMPTZ
);

CREATE INDEX idx_tunnel_tokens_hash ON tunnel_tokens(token_hash);
CREATE INDEX idx_tunnel_tokens_tunnel ON tunnel_tokens(tunnel_id);
CREATE INDEX idx_tunnel_tokens_expires ON tunnel_tokens(expires_at)
    WHERE revoked_at IS NULL;
```

## Authentication Flows

### API Key Authentication (HTTP)

```
Client                                Server
  │                                     │
  │── GET /api/v1/sessions ────────────►│
  │   Authorization: Bearer mk_xxx...   │
  │                                     │
  │                            ┌────────┴────────┐
  │                            │ 1. Extract token│
  │                            │ 2. SHA-256 hash │
  │                            │ 3. Lookup by    │
  │                            │    hash         │
  │                            │ 4. Check status │
  │                            │ 5. Inject       │
  │                            │    tenant_id    │
  │                            └────────┬────────┘
  │                                     │
  │◄── 200 OK ──────────────────────────│
```

### Runner Token Authentication (gRPC)

```
Runner                                Server
  │                                     │
  │── Connect() ───────────────────────►│
  │   metadata: x-runner-token=rtok_xxx │
  │                                     │
  │                            ┌────────┴────────┐
  │                            │ 1. Verify token │
  │                            │ 2. Check status │
  │                            │ 3. Bind runner  │
  │                            │    to session   │
  │                            └────────┬────────┘
  │                                     │
  │◄── Stream established ──────────────│
```

### Tunnel Token Authentication

```
User                   Server                    Runner
  │                      │                         │
  │── Create tunnel ────►│                         │
  │                      │── Generate ttok_xxx ───►│
  │◄── Tunnel URL + token│                         │
  │                      │                         │
  │── Connect to tunnel ─┼─────────────────────────┤
  │   ?token=ttok_xxx    │                         │
  │                      │                         │
```

## Token Lifecycle

### Creation

```bash
# API Key
mctl admin keys create --name "ci-pipeline" --scopes "tasks:*"
# Output: mk_dGhpcyBpcyBhIHRlc3Q... (shown once, store securely)

# Runner Token
mctl admin runner-tokens create --pool macos-pool
# Output: rtok_YW5vdGhlciB0ZXN0... (shown once)
```

### Rotation (Runner Tokens)

```
┌─────────────────────────────────────────────────────────────────────┐
│                     Token Rotation Flow                             │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  1. Generate new token                                              │
│     new_hash = SHA256(new_token)                                    │
│                                                                     │
│  2. Update database                                                 │
│     previous_token_hash = current token_hash                        │
│     token_hash = new_hash                                           │
│     rotation_deadline = NOW() + 1 hour                              │
│     status = 'rotating'                                             │
│                                                                     │
│  3. During rotation window (1 hour)                                 │
│     Both old and new tokens are valid                               │
│                                                                     │
│  4. After rotation_deadline                                         │
│     previous_token_hash = NULL                                      │
│     status = 'active'                                               │
│     Old token no longer valid                                       │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### Revocation

```bash
# Immediate revocation
mctl admin keys revoke key_xxx --reason "compromised"

# All tokens for a runner
mctl admin runner-tokens revoke --runner run_xxx
```

## Security Considerations

### What We Store

| Data | Storage | Notes |
|------|---------|-------|
| Token | **Never stored** | Only shown once at creation |
| Hash | Database | SHA-256 hex, 64 characters |
| Prefix | Database | First 8 chars for identification |

### Database Leak Scenario

If the database is compromised:

1. **Attacker cannot recover tokens**: SHA-256 is one-way
2. **Attacker cannot forge tokens**: Would need to guess 256-bit random value
3. **Tokens should still be rotated**: Defense in depth

### Future: HMAC Upgrade

If additional security is needed:

1. Set `CurrentHashVersion = HashV2_HMAC_SHA256`
2. Configure `MARIONETTE_TOKEN_KEY` environment variable
3. New tokens use HMAC-SHA256
4. Old tokens (version=1) continue to work
5. Optional: Background job to rotate all tokens to v2

```go
// Future HMAC configuration
security:
  token:
    hash_version: 2  # Use HMAC-SHA256
    hmac_key_file: /etc/marionette/token.key
```

## Agent API Keys (Encrypted Storage)

Agent API keys (for Claude, Codex, etc.) are **encrypted**, not hashed, because the server needs to decrypt and use them.

See [Storage Documentation](storage.md) for encryption details:
- Envelope encryption (KEK/DEK)
- Per-tenant data encryption keys
- AES-256-GCM

```sql
CREATE TABLE agent_configs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    agent TEXT NOT NULL,              -- claude, codex, gemini
    api_key_encrypted TEXT NOT NULL,  -- AES-256-GCM encrypted
    -- ...
);
```
