# Contributing to Revenium Go SDK

Thank you for your interest in contributing!

## Development Setup

### Prerequisites

- Go 1.22 or later
- Git

### Getting Started

1. Fork the repository
2. Clone your fork:
   ```bash
   git clone https://github.com/your-username/revenium-go-sdk.git
   cd revenium-go-sdk
   ```
3. Set up the workspace:
   ```bash
   go work sync
   ```
4. Install dependencies:
   ```bash
   make deps
   ```

### Running Tests

```bash
make test-all

cd openai && go test -v -race ./...
```

### Linting and Formatting

```bash
make lint-all
make fmt-all
```

## Project Structure

- `core/` -- Shared module: config, errors, logging, metering client, resilience, prompt capture, job tracking
- `openai/`, `anthropic/`, `google/`, etc. -- Provider-specific middleware modules
- Each provider has its own `go.mod` importing `core` and its upstream SDK
- `go.work` ties all modules together for local development

## How to Add a New Provider

1. Create a new directory: `myprovider/`
2. Initialize the module: `cd myprovider && go mod init github.com/revenium/revenium-go-sdk/myprovider`
3. Add a `replace` directive for local core: `go mod edit -replace github.com/revenium/revenium-go-sdk/core=../core`
4. Implement the `Initialize(opts ...Option) error` and `GetClient() (*ReveniumMyProvider, error)` pattern
5. Add metering integration using `core/metering`
6. Add the module to `go.work`
7. Add the module to the CI matrix in `.github/workflows/test.yml`
8. Add tests using `github.com/stretchr/testify`
9. Update `README.md` with provider documentation

## Pull Request Process

1. Create a feature branch from `main`
2. Keep changes focused and atomic
3. Ensure all tests pass: `make test-all`
4. Ensure code passes vet: `make lint-all`
5. Write clear commit messages
6. Submit a pull request with a description of the changes

## Code Style

- Clean, readable code with expressive naming
- No inline comments -- code should be self-documenting
- Use `github.com/stretchr/testify` for test assertions
- Follow existing patterns in the codebase
- Single responsibility per function
- Prefer composition over inheritance

## What to Contribute

- Bug fixes
- New provider integrations
- Test coverage improvements
- Performance optimizations
- Documentation improvements

## Security

For security vulnerabilities, follow our [Security Policy](SECURITY.md) -- do not create public issues.

## License

By contributing, you agree your contributions will be licensed under the [MIT License](LICENSE).
