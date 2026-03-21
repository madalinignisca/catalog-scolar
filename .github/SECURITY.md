# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| main    | Yes       |

## Reporting a Vulnerability

If you discover a security vulnerability in CatalogRO, please report it responsibly:

1. **Do NOT** open a public GitHub issue
2. Email: security@catalogro.ro (or use GitHub's private vulnerability reporting)
3. Include: description, reproduction steps, impact assessment
4. We will acknowledge within 48 hours and provide a fix timeline within 7 days

## Security Measures

- Row-Level Security (RLS) enforces tenant isolation at the database level
- All dependencies are monitored via Dependabot
- Pre-commit hooks run gitleaks (secret scanning), gosec, govulncheck, npm audit, and semgrep
- GDPR compliance: student data is never hard-deleted, audit logs are immutable
