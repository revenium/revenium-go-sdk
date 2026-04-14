# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 1.x     | Yes                |

## Reporting a Vulnerability

If you discover a security vulnerability, please report it responsibly.

**DO NOT** create a public GitHub issue for security vulnerabilities.

### How to Report

Email: **support@revenium.io**

Include:

- Package name and version
- Description of the vulnerability
- Steps to reproduce (if applicable)
- Potential impact
- Suggested fix (if available)

We will review and respond to security reports within 5 business days.

## Security Best Practices

When using this SDK:

1. **API Keys**: Never commit API keys to version control. Use `.env` files or environment variables.
2. **Environment Variables**: Store all sensitive configuration via environment variables.
3. **Prompt Capture**: Disabled by default. When enabled, PII is automatically sanitized before transmission.
4. **Network Security**: All connections to Revenium APIs use HTTPS.
5. **Updates**: Keep the SDK updated to the latest version for security patches.

## Data Transmitted

The SDK transmits the following data to Revenium for metering:

- Provider name and model identifier
- Token counts (input, output, total)
- Request latency and timing metrics
- Transaction identifiers
- Stop reasons
- Optionally: sanitized prompt content (when `REVENIUM_CAPTURE_PROMPTS=true`)

No raw API keys or credentials are transmitted to Revenium.

## Additional Resources

- [Revenium Documentation](https://docs.revenium.io)
- [Revenium Website](https://www.revenium.ai)
