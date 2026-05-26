# Example Project

A short description of the project. This is the canonical README for our example
project, demonstrating typical sections like installation, usage, API reference,
contributing guidelines, and licensing terms.

## Installation

Install via Go modules:

```bash
go install github.com/example/project@latest
```

Or via Homebrew on macOS:

```bash
brew install example/tap/project
```

## Usage

Run the binary with the required configuration:

```bash
example --flag value --other-flag other-value
```

By default the binary reads `~/.config/example/config.toml`. Override via the
`EXAMPLE_CONFIG` environment variable.

## API Reference

### `func Hello(name string) string`

Returns a friendly greeting given a name. The greeting is internationalized
based on the current locale setting via `LANG` environment variable. If the
name is empty the function returns `"Hello, stranger!"`.

### `func Goodbye(name string) string`

Returns a polite farewell given a name. Symmetric counterpart to `Hello`.
Returns `"Goodbye, stranger!"` for empty name.

### `type Config struct`

The main configuration shape consumed by `Run`. Fields:

- `Verbose bool` — enable debug logging
- `Timeout time.Duration` — operation timeout
- `Logger *slog.Logger` — structured logger handle

## Contributing

See `CONTRIBUTING.md` for full guidelines. In short: open an issue first,
discuss the design, then submit a PR with tests and documentation.

## License

Released under the MIT license. See `LICENSE` for full text.
