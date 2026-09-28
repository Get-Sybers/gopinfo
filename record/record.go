// Package record is the common record shape of the Linux Go tools: the
// envelope every record carries (docs/linux §3.3), the JSONL writer, and
// the stage-manifest join that stamps provenance — Origin, Snapshot,
// Residue — onto records without any tool computing it (rule 2).
//
// JSONL is the one output format of the Linux matrix: one JSON object per
// record, nothing else. A tool that ever needs interim storage beyond
// streaming (sorting, aggregation past memory) uses a database format in
// its WORK_DIR scratch — never an interchange text format — and the record
// files stay JSONL.
//
// The envelope carries no synthetic identifiers: record identity is
// natural fields only, and byakugan's guids never appear in parser output
// (rule 1).
package record

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Origin is where the parsed file existed in the evidence — the dfVFS
// path-spec role: the image, the volume (partition index or vg/lv), the
// original volume path and inode. It is stamped by the batch runtime from
// the access layer's manifest; tool code never computes it.
type Origin struct {
	Image  string `json:"Image,omitempty"`
	Volume string `json:"Volume,omitempty"`
	// FSUUID and Label are the volume's durable identity — the role a drive
	// serial plays on Windows: what fstab/crypttab rows and mount units name.
	FSUUID string `json:"FSUUID,omitempty"`
	Label  string `json:"Label,omitempty"`
	Path   string `json:"Path,omitempty"`
	Inode  uint64 `json:"Inode,omitempty"`
}

// Snapshot marks a record that came out of a snapshot (docs/linux §6.4);
// absent means the live volume.
type Snapshot struct {
	Backend string `json:"Backend,omitempty"`
	ID      string `json:"ID,omitempty"`
	Name    string `json:"Name,omitempty"`
	Time    string `json:"Time,omitempty"`
}

// Residue marks a record recovered from filesystem residue (docs/linux
// §5.5); absent means an ordinary allocated file.
type Residue struct {
	Kind   string `json:"Kind,omitempty"`
	Detail string `json:"Detail,omitempty"`
}

// Host is the imaged host's own identity, stamped from the Layer-1
// knowledge store (docs/linux §4, decision 14): the image's hostname,
// machine id, OS and the timezone that was applied to naive timestamps.
// Image-self-knowledge, never an inference.
type Host struct {
	Hostname  string `json:"Hostname,omitempty"`
	MachineID string `json:"MachineID,omitempty"`
	OS        string `json:"OS,omitempty"`
	Timezone  string `json:"Timezone,omitempty"`
}

// Envelope is the common head of every record. Tool + RecordType is the
// record's parser chain (the role plaso's Parser field plays);
// SourceFilename is the parsed file relative to the input root; EventTime
// is ISO 8601 UTC (fixed microseconds, tstamp.ISO8601Layout) and TimeKind says what the time is when an artefact
// carries several.
type Envelope struct {
	Tool           string    `json:"Tool"`
	ToolVersion    string    `json:"ToolVersion"`
	RecordType     string    `json:"RecordType"`
	SourceFilename string    `json:"SourceFilename"`
	SourceModified string    `json:"SourceModified,omitempty"`
	EventTime      string    `json:"EventTime,omitempty"`
	TimeKind       string    `json:"TimeKind,omitempty"`
	Origin         *Origin   `json:"Origin,omitempty"`
	Snapshot       *Snapshot `json:"Snapshot,omitempty"`
	Residue        *Residue  `json:"Residue,omitempty"`
	Host           *Host     `json:"Host,omitempty"`
}

// Env makes an embedded Envelope satisfy the Record interface.
func (e *Envelope) Env() *Envelope { return e }

// Record is what a tool hands the writer: its own struct embedding
// Envelope.
type Record interface {
	Env() *Envelope
}

// Stamp is what the runtime knows about the item being processed. Applied
// fill-only-blank, so a tool that sets a field itself (the --tar path sets
// SourceFilename to the volume path) is never overwritten.
type Stamp struct {
	Tool           string
	ToolVersion    string
	SourceFilename string
	SourceModified string
	Origin         *Origin
	Snapshot       *Snapshot
	Residue        *Residue
	Host           *Host
}

func (s Stamp) apply(e *Envelope) {
	if e.Tool == "" {
		e.Tool = s.Tool
	}
	if e.ToolVersion == "" {
		e.ToolVersion = s.ToolVersion
	}
	if e.SourceFilename == "" {
		e.SourceFilename = s.SourceFilename
	}
	if e.SourceModified == "" {
		e.SourceModified = s.SourceModified
	}
	if e.Origin == nil {
		e.Origin = s.Origin
	}
	if e.Snapshot == nil {
		e.Snapshot = s.Snapshot
	}
	if e.Residue == nil {
		e.Residue = s.Residue
	}
	if e.Host == nil {
		e.Host = s.Host
	}
}

// Writer writes a tool's records as JSONL — one JSON object per line —
// stamping the envelope on every record.
type Writer struct {
	stamp Stamp
	bw    *bufio.Writer
	jw    *json.Encoder
	n     int
}

// NewWriter builds the JSONL writer over w.
func NewWriter(w io.Writer) *Writer {
	rw := &Writer{bw: bufio.NewWriter(w)}
	rw.jw = json.NewEncoder(rw.bw)
	rw.jw.SetEscapeHTML(false)
	return rw
}

// SetStamp installs the per-item stamp the runtime resolved.
func (rw *Writer) SetStamp(s Stamp) { rw.stamp = s }

// Write stamps and emits one record. The count moves only on a
// successful encode, so Count never over-reports into the summary.
func (rw *Writer) Write(rec Record) error {
	rw.stamp.apply(rec.Env())
	if err := rw.jw.Encode(rec); err != nil {
		return err
	}
	rw.n++
	return nil
}

// Count is the number of records written.
func (rw *Writer) Count() int { return rw.n }

// Flush commits buffered output; call before closing the underlying file.
func (rw *Writer) Flush() error { return rw.bw.Flush() }

// ---- the stage-manifest join (rule 2) --------------------------------------

// ManifestName is the access layer's manifest file at the input root:
// one JSON object per staged file (`gomount materialise --manifest`).
const ManifestName = "materialise.jsonl"

// Manifest resolves a staged path to its origin chain. Rows are parsed
// permissively: today's gomount fields (path, size, mtime, mftid) and the
// docs/linux §5.4 origin-record fields (image, volume, inode, snapshot{},
// residue{}, staged) are all understood, unknown keys are ignored, and a
// row without a path is read as manifest-level metadata (image/volume
// defaults).
type Manifest struct {
	image  string
	volume string
	fsuuid string
	label  string
	rows   map[string]manifestRow
}

type manifestRow struct {
	origin   Origin
	snapshot *Snapshot
	residue  *Residue
}

// LoadManifest reads root/materialise.jsonl. A missing or unreadable
// manifest is not an error — provenance is simply absent (loose evidence):
// it returns nil, and a nil *Manifest is safe to use.
func LoadManifest(root string) *Manifest {
	f, err := os.Open(filepath.Join(root, ManifestName))
	if err != nil {
		return nil
	}
	defer f.Close()
	m := &Manifest{rows: map[string]manifestRow{}}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		// UseNumber keeps 64-bit inode/mftid values exact — plain
		// Unmarshal would coerce them to float64 and lose precision
		// past 2^53.
		dec := json.NewDecoder(strings.NewReader(line))
		dec.UseNumber()
		var raw map[string]any
		if dec.Decode(&raw) != nil {
			continue
		}
		m.addRow(raw)
	}
	if len(m.rows) == 0 && m.image == "" && m.volume == "" {
		return nil
	}
	return m
}

func (m *Manifest) addRow(raw map[string]any) {
	get := func(k string) string {
		if v, ok := raw[k].(string); ok {
			return v
		}
		return ""
	}
	getU := func(keys ...string) uint64 {
		for _, k := range keys {
			switch v := raw[k].(type) {
			case json.Number: // the LoadManifest path: exact
				if n, err := strconv.ParseUint(v.String(), 10, 64); err == nil && n > 0 {
					return n
				}
			case float64: // a caller without UseNumber
				if v > 0 {
					return uint64(v)
				}
			}
		}
		return 0
	}
	path := get("path")
	if path == "" {
		// manifest-level metadata row
		if v := get("image"); v != "" {
			m.image = v
		}
		if v := get("volume"); v != "" {
			m.volume = v
		}
		if v := get("fsuuid"); v != "" {
			m.fsuuid = v
		}
		if v := get("label"); v != "" {
			m.label = v
		}
		return
	}
	row := manifestRow{origin: Origin{
		Image:  get("image"),
		Volume: get("volume"),
		FSUUID: get("fsuuid"),
		Label:  get("label"),
		Path:   volumePath(path),
		Inode:  getU("inode", "mftid"),
	}}
	if s, ok := raw["snapshot"].(map[string]any); ok {
		row.snapshot = &Snapshot{
			Backend: str(s["backend"]), ID: str(s["id"]),
			Name: str(s["name"]), Time: str(s["time"]),
		}
	}
	if r, ok := raw["residue"].(map[string]any); ok {
		row.residue = &Residue{Kind: str(r["kind"]), Detail: str(r["detail"])}
	}
	key := get("staged")
	if key == "" {
		key = path
	}
	m.rows[normKey(key)] = row
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// normKey folds a staged path to the lookup key: forward slashes, no
// leading separators, lower-cased (staged trees out of NTFS are
// case-insensitive).
func normKey(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimLeft(p, "/")
	return strings.ToLower(p)
}

// volumePath renders the manifest's path as the original volume path, with
// a leading slash and forward separators.
func volumePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	return "/" + strings.TrimLeft(p, "/")
}

// Stamp resolves the staged path rel (relative to the input root) to its
// provenance, or all-nil when unknown. Safe on a nil Manifest.
func (m *Manifest) Stamp(rel string) (*Origin, *Snapshot, *Residue) {
	if m == nil {
		return nil, nil, nil
	}
	row, ok := m.rows[normKey(rel)]
	if !ok {
		return nil, nil, nil
	}
	o := row.origin
	if o.Image == "" {
		o.Image = m.image
	}
	if o.Volume == "" {
		o.Volume = m.volume
	}
	if o.FSUUID == "" {
		o.FSUUID = m.fsuuid
	}
	if o.Label == "" {
		o.Label = m.label
	}
	return &o, row.snapshot, row.residue
}
