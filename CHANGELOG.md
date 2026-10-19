# Changelog

All notable changes are documented here, following
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Evidence-asset identity on the record envelope: `record.Origin` gains
  `ImageSHA256`, `DiskSerial`, `DiskID` (image/disk-scoped) and `VolumeGUID`,
  `PartGUID`, `PartOffset` (volume-scoped), additive and `omitempty`. The
  `record.Manifest` join reads the matching `gomount` manifest keys
  (`image_sha256`, `disk_serial`, `disk_id` on the path-less metadata row;
  `volume_guid`, `part_guid`, `part_offset` per row) and fills them
  fill-only-blank, exactly like the existing `image`/`volume`/`fsuuid`/`label`
  path (the frozen gopinfo↔gomount Origin field contract).
- An optional evidence-identity stamp hook on the `framework` runtime
  (`Tool.Stamp`, resolved per item and exposed on `Config.Stamp`): a bound
  tool that writes its own record files — gowindowlicker, signatures, zeek —
  can supply a per-item `Origin` and `Host` to brand its records with, the
  framework analogue of the batch runtime's manifest join. Opt-in; a tool that
  sets no hook is unaffected.
- The conformance gate of the get-sybers standard (DX_DFIR
  `docs/reference/build-and-test.md`): `make conform` (gofmt, vet, build,
  shuffled tests with coverage, `go mod tidy -diff`, cross-compile targets, a
  `go:generate` drift check and `scripts/structlint.sh`), `make lint`
  (golangci-lint v2, `.golangci.yml`) and `make vuln` (govulncheck), run by the
  `CI` workflow on pull requests and `main` with `all_green` as the one
  required status check.
- Repository hygiene: `.editorconfig`, `.gitattributes`, `.shellcheckrc`,
  Dependabot (actions and Go modules, weekly, grouped), `CODEOWNERS`, a
  security policy and a pull-request template.

### Fixed
- The stale `gopinfo.Version` constant (`0.1.0` → `0.3.0`), so the batch
  summary's `pinfo` key and `pinfo-report --version` report the real runtime
  version the record tree was produced by.
- The findings the new lint gate surfaced: unchecked writes made explicit,
  `errors.Is` for the `io.EOF` comparison in `plist`, and a test fixture built
  with `slices.Concat` instead of an aliasing `append`.

## [0.2.1] - 2026-10-03

The last release before this changelog was started; see the git history for
this and the earlier tags.

## [0.2.0] - 2026-10-03

## [0.1.0] - 2026-09-28

[Unreleased]: https://github.com/Get-Sybers/gopinfo/compare/v0.2.1...HEAD
[0.2.1]: https://github.com/Get-Sybers/gopinfo/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/Get-Sybers/gopinfo/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/Get-Sybers/gopinfo/releases/tag/v0.1.0
