# Changelog

All notable changes are documented in this file.

## [1.22.0] - 2026-02-06

### Added
- Range over function types `func(yield func(K, V) bool)`.
- New `slices.Concat` for joining multiple slices.

### Changed
- For-loop variables are now per-iteration scoped (closes long-standing footgun).

## [1.21.0] - 2023-08-08

### Added
- Built-in functions `min`, `max`, `clear`.
- New `log/slog` structured logging package.

### Fixed
- `time.Now` UTC offset jitter on macOS arm64.
