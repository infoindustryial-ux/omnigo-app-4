# Changelog

## [1.0.1] - 2026-07-22

### Added
- Example checkout app under `example/`
- Unit test suite: token models, token API client (stubbed HTTP adapter),
  builder validation and amount formatting
- `analysis_options.yaml`, so the declared `flutter_lints` rules are enforced
- Package `.gitignore`; build artefacts are no longer tracked or shipped
- iOS platform setup, example, and test instructions in the README

### Changed
- Minimum Flutter raised to `3.10.0`. The SDK uses `BuildContext.mounted`
  (Flutter 3.7+) and the previous `>=3.0.0` constraint allowed versions that
  could not compile it.
- `RedirectionServiceBuilder.setAmount` accepts decimal strings and rounds them
  to a whole unit, throwing `ArgumentError` on non-numeric input. It previously
  threw an unguarded `FormatException` on any decimal amount.
- Git dependency snippet in the README now points at the real repository, with
  the `path:` the package needs
- Distribution archive rebuilt without `.DS_Store`, `__MACOSX/`, `.dart_tool/`
  and `pubspec.lock`

### Removed
- Unused `http` dependency; all HTTP calls go through `dio`

## [1.0.0] - 2025-12-02

### Added
- Initial release of Payfast Flutter SDK
- Token generation API service
- WebView-based payment redirection
- Builder pattern for RedirectionService
- Success and failure callbacks
- Support for UAT and production environments
- API documentation in the README
