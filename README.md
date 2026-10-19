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

## Evidence-asset identity on the record envelope

Every record's `Origin` can carry the identity of the evidence asset it came
from, joined from the stage manifest (`materialise.jsonl`) at one shared stamp
site — tool code never computes it. Alongside the existing `Image`, `Volume`,
`FSUUID` and `Label`, `Origin` carries (additive, `omitempty`):

| Field | Scope | Meaning |
|---|---|---|
| `ImageSHA256` | image | sha256 of the acquisition/container bytes |
| `DiskSerial` | image | drive/device serial (EWF `sn`, VM container uuid); omitted for raw/dd/QCOW2 |
| `DiskID` | disk | GPT disk GUID, or MBR disk signature (hex) |
| `VolumeGUID` | volume | volume GUID identity: Linux/mac `FSUUID` when it is a canonical 128-bit UUID; empty on Windows at extraction |
| `PartGUID` | volume | GPT unique partition GUID; omitted on MBR |
| `PartOffset` | volume | partition byte offset |

A field absent for a format or OS is omitted, never faked. The host's own
identity is **not** an `Origin` field — it rides `Host.MachineID`. `gomount`
writes the matching manifest keys (`image_sha256`, `disk_serial`, `disk_id` on
the path-less metadata row; `volume_guid`, `part_guid`, `part_offset` per row),
which `record.Manifest` fills fill-only-blank — the frozen gopinfo↔gomount
Origin field contract.

Tools built on the `framework` runtime (those that write their own record
files rather than through a stamping `record.Writer`) can opt in to the same
provenance by setting `Tool.Stamp`: the runtime resolves it per item and
exposes the `Origin` and `Host` on `Config.Stamp`.

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
