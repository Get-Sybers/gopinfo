// Package gopinfo is the shared parser-infrastructure module of the get-sybers
// DFIR Go tools: the batch runtime (gopinfo/batch), the record envelope and
// writers (gopinfo/record), timestamp normalisation (gopinfo/tstamp), discovery
// helpers (gopinfo/discover) and the `gomount stream` tar consumer
// (gopinfo/tarstream).
//
// The module carries no synthetic identifiers and derives nothing: parsers
// extract, byakugan derives. It is imported by path
// (github.com/Get-Sybers/gopinfo/...) and versioned as a Go module; Version
// below is reported on every batch summary line.
package gopinfo

// Version is the gopinfo runtime version, reported as the batch summary
// line's "pinfo" key (and by `pinfo-report --version`) so a record tree
// states which runtime produced it. It tracks the released module tag — the
// one home for the version, bumped with each release — so an engine that
// pins gopinfo reports the version it compiled against.
const Version = "0.4.0"
