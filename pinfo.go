// Package pinfo is the shared parser-infrastructure module of the GoDFIR-toolz
// Go tools (docs/linux §3): the batch runtime (pinfo/batch), the record
// envelope and writers (pinfo/record), timestamp normalisation (pinfo/tstamp),
// discovery helpers (pinfo/discover) and the `gomount stream` tar consumer
// (pinfo/tarstream).
//
// The module carries no synthetic identifiers and derives nothing: parsers
// extract, byakugan derives (docs/linux, the rules). It is consumed via a
// directory replace from the sibling tool directories and versioned by the
// checked-out tree; Version below is reported on every batch summary line.
package gopinfo

// Version is the pinfo runtime version, reported as the summary line's
// "pinfo" key so a record tree states which runtime produced it.
const Version = "0.1.0"
