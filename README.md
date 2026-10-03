# gopinfo

A Go port of the [log2timeline/plaso](https://github.com/log2timeline/plaso)
`pinfo` idea, and the **shared core** of the get-sybers DFIR Go tooling.

`gopinfo` is one importable module — `github.com/Get-Sybers/gopinfo` — that the
tools (`gowindowlicker`, `godaemonhunter`, `gomount`, …) import instead of
copying shared code between repositories (see `go-standards.md` §7). It is
**standard library only**: no external dependencies.

## Install

```bash
go get github.com/Get-Sybers/gopinfo@latest
```

```go
import "github.com/Get-Sybers/gopinfo/framework"
```

## Packages

| Package | Purpose |
|---|---|
| `framework` | the batch runtime the get-sybers tools share: env contract, discovery loop, record files, idempotency, one summary line, exit codes |
| `batch` | the batch runtime whose `Process` writes through a provenance-stamping `*record.Writer` |
| `record` | the common record shape and writer of the Linux Go tools |
| `diskimage` | disk-image discovery, selection, naming and materialise, shared by the image-consuming tools |
| `discover` | shared input-discovery helpers: deterministic ordering and compressed-stream sniffing |
| `knowledge` | the image's Layer-1 knowledge store |
| `plist` | decodes Apple property lists — the XML and binary forms |
| `families` | the typed-event engine of the Linux matrix |
| `tarstream` | consumes the `gomount stream` pipe (a tar archive on stdin) |
| `tstamp` | normalises artefact timestamps to the envelope's ISO 8601 form |
| `toolkit` | small, behaviour-identical primitives shared across the batch runtimes |
| `report` | answers "what did this evidence produce?" over a tool run |

The `cmd/pinfo-report` command prints a run's report. See each package's doc
comment for detail.

## Build & test

Standard library only, so the module builds and tests standalone:

```bash
go build ./...
go vet ./...
go test ./...
```

## Consuming it locally (before a version is tagged)

Consumers pin a version with `require github.com/Get-Sybers/gopinfo vX.Y.Z`.
While iterating across sibling checkouts, add a local `replace` to the
consuming module — the get-sybers convention for in-development shared code
(`go-standards.md` §2):

```
require github.com/Get-Sybers/gopinfo v0.0.0
replace github.com/Get-Sybers/gopinfo => ../gopinfo
```

Drop the `replace` once the version you need is tagged.
