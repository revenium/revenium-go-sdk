# Contributing to Revenium Go SDK

Thank you for your interest in contributing!

## Development Setup

### Prerequisites

- Go 1.25 or later (the version `go.work` declares; each published module's own minimum is listed under Requirements in the README)
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

### How Tests Are Sandboxed

No test may reach Revenium or any other real server. Every test package that can make a network call has a `TestMain` that calls `sandbox.Run` from `core/testutil/sandbox`:

```go
func TestMain(m *testing.M) {
	os.Exit(sandbox.Run(m))
}
```

`sandbox.Run` points `REVENIUM_METERING_BASE_URL` and `REVENIUM_BASE_URL` at a local port nothing listens on, clears the Revenium keys, team ID and cost-control address inherited from your shell, drops any `HTTP_PROXY` or `HTTPS_PROXY` that points off the machine, and blocks every connection to a host that is not `localhost` or a loopback address, including connections the SDK's own clients open to an IP address. A blocked attempt is printed, with the test that made it where the SDK can tell, and fails the package even when every test passed. Point clients at an `httptest` server; names reserved by RFC 2606 and RFC 6761 (`.invalid`, `.test`, `.example`, `example.com`) are blocked without failing, for tests that need an address that never answers.

CI also runs each module's tests through `scripts/ci/run-with-egress-sink.sh`, which sets `HTTP_PROXY` and `HTTPS_PROXY` to a local proxy that records and refuses every request, and fails the job if it recorded any. Run the same check locally with `scripts/ci/run-with-egress-sink.sh make test-all`.

When you add a test package, add the `TestMain` above to it (a module with no network code and no dependency on `core`, such as `webhooks`, relies on the CI check alone).

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
3. Require core at the version of the newest heading in `CHANGELOG.md` (the release in preparation) and add a `replace` directive for local core: `go mod edit -require github.com/revenium/revenium-go-sdk/core@vX.Y.Z -replace github.com/revenium/revenium-go-sdk/core=../core`. Consumers ignore `replace`, so a module released with an older core requirement builds against that older core
4. Implement the `Initialize(opts ...Option) error` and `GetClient() (*ReveniumMyProvider, error)` pattern
5. Add metering integration using `core/metering`
6. Add the module to `go.work`
7. Add the module to both module lists in `.github/workflows/test.yml`: the `test` job and the `test-minimum-go` job
8. Add a row with the module's `go` line to the `| Module | Minimum Go |` table in `README.md`, `llms.txt` and `llms-full.txt`; if it moves the lowest or highest floor, update the range text and the README badge too
9. Add tests using `github.com/stretchr/testify`, with the sandbox `TestMain` described under How Tests Are Sandboxed in each test package
10. Update `README.md` with provider documentation
11. Run `scripts/ci/check-go-versions.sh` and `scripts/ci/check-core-requirement.sh` before opening the PR; CI runs the same checks. When a new version heading is added to `CHANGELOG.md`, raise the core requirement in every provider `go.mod` to match

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
