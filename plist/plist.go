// Package plist decodes Apple property lists — the XML form and the
// binary "bplist00" form — into plain Go values, for the macOS parsers of
// the daemon matrix (launchd jobs, dslocal accounts, SystemVersion, the
// SystemConfiguration preferences). Clean-room, from Apple's published
// formats; decode only.
//
// Values: a dict is map[string]any, an array []any, and the scalars are
// string, int64, float64, bool, time.Time (UTC), []byte and UID (the
// keyed-archiver object reference). Nothing is coerced: the accessors
// (Dict, Strings, Int, Bool, …) are the parsers' way to read a value that
// may be a scalar or a one-element array, the way dslocal stores every
// attribute.
package plist

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

// UID is a keyed-archiver object reference (the CF$UID of NSKeyedArchiver).
type UID uint64

const (
	binaryMagic = "bplist00"
	trailerSize = 32
	maxDepth    = 512
	// MaxSize bounds what Decode accepts: property lists are configuration,
	// not bulk data.
	MaxSize = 64 << 20
)

// appleEpoch is 2001-01-01T00:00:00Z, the zero of a plist date.
var appleEpoch = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

// IsBinary reports the bplist00 signature.
func IsBinary(b []byte) bool { return len(b) >= 8 && string(b[:8]) == binaryMagic }

// Decode parses either form, chosen by content.
func Decode(b []byte) (any, error) {
	if len(b) > MaxSize {
		return nil, fmt.Errorf("plist: %d bytes exceeds the %d-byte limit", len(b), MaxSize)
	}
	if IsBinary(b) {
		return DecodeBinary(b)
	}
	return DecodeXML(b)
}

// ---- binary ------------------------------------------------------------------------

type binDecoder struct {
	b        []byte
	offSize  int
	refSize  int
	nObjects uint64
	table    uint64
	visiting map[uint64]bool
}

// DecodeBinary parses a bplist00 property list. Every offset and count is
// bounds-checked; a reference cycle or a nesting deeper than maxDepth is
// an error.
func DecodeBinary(b []byte) (any, error) {
	if !IsBinary(b) {
		return nil, errors.New("plist: not a binary property list")
	}
	if len(b) < 8+trailerSize {
		return nil, errors.New("plist: binary property list too short for its trailer")
	}
	t := b[len(b)-trailerSize:]
	d := &binDecoder{
		b:        b,
		offSize:  int(t[6]),
		refSize:  int(t[7]),
		nObjects: binary.BigEndian.Uint64(t[8:16]),
		table:    binary.BigEndian.Uint64(t[24:32]),
		visiting: map[uint64]bool{},
	}
	top := binary.BigEndian.Uint64(t[16:24])
	if d.offSize < 1 || d.offSize > 8 || d.refSize < 1 || d.refSize > 8 {
		return nil, fmt.Errorf("plist: offset size %d / reference size %d", d.offSize, d.refSize)
	}
	if d.nObjects == 0 || d.nObjects > uint64(len(b)) {
		return nil, fmt.Errorf("plist: %d objects in %d bytes", d.nObjects, len(b))
	}
	if d.table < 8 || d.table+d.nObjects*uint64(d.offSize) > uint64(len(b)-trailerSize) {
		return nil, errors.New("plist: offset table outside the file")
	}
	if top >= d.nObjects {
		return nil, fmt.Errorf("plist: top object %d of %d", top, d.nObjects)
	}
	return d.object(top, 0)
}

// beUint reads an n-byte big-endian unsigned integer.
func beUint(b []byte) uint64 {
	var v uint64
	for _, x := range b {
		v = v<<8 | uint64(x)
	}
	return v
}

func (d *binDecoder) offset(ref uint64) (int, error) {
	if ref >= d.nObjects {
		return 0, fmt.Errorf("plist: object reference %d of %d", ref, d.nObjects)
	}
	at := int(d.table) + int(ref)*d.offSize
	off := beUint(d.b[at : at+d.offSize])
	if off >= uint64(len(d.b)-trailerSize) {
		return 0, fmt.Errorf("plist: object %d at %d, outside the file", ref, off)
	}
	return int(off), nil
}

// count reads a marker's element count: the low nibble, or — when it is
// 0xF — the integer object that follows. It returns the count and the
// offset of the payload.
func (d *binDecoder) count(off int) (int, int, error) {
	marker := d.b[off]
	n := int(marker & 0x0f)
	if n != 0x0f {
		return n, off + 1, nil
	}
	if off+1 >= len(d.b)-trailerSize || d.b[off+1]&0xf0 != 0x10 {
		return 0, 0, fmt.Errorf("plist: count marker without an integer at %d", off)
	}
	size := 1 << (d.b[off+1] & 0x0f)
	if size > 8 || off+2+size > len(d.b)-trailerSize {
		return 0, 0, fmt.Errorf("plist: count integer of %d bytes at %d", size, off)
	}
	v := beUint(d.b[off+2 : off+2+size])
	if v > uint64(len(d.b)) {
		return 0, 0, fmt.Errorf("plist: count %d at %d exceeds the file", v, off)
	}
	return int(v), off + 2 + size, nil
}

func (d *binDecoder) object(ref uint64, depth int) (any, error) {
	if depth > maxDepth {
		return nil, errors.New("plist: nesting too deep")
	}
	off, err := d.offset(ref)
	if err != nil {
		return nil, err
	}
	limit := len(d.b) - trailerSize
	marker := d.b[off]
	switch marker >> 4 {
	case 0x0:
		switch marker {
		case 0x00:
			return nil, nil
		case 0x08:
			return false, nil
		case 0x09:
			return true, nil
		case 0x0f:
			return nil, nil // fill
		}
		return nil, fmt.Errorf("plist: unknown marker %#x at %d", marker, off)
	case 0x1: // integer, 2^n bytes
		size := 1 << (marker & 0x0f)
		if size > 16 || off+1+size > limit {
			return nil, fmt.Errorf("plist: integer of %d bytes at %d", size, off)
		}
		raw := d.b[off+1 : off+1+size]
		if size == 16 { // 128-bit: the low 64 bits carry the value Apple writes
			raw = raw[8:]
		}
		v := beUint(raw)
		if size == 8 || size == 16 {
			return int64(v), nil
		}
		return int64(v), nil // 1, 2, 4 bytes are unsigned
	case 0x2: // real
		size := 1 << (marker & 0x0f)
		if off+1+size > limit {
			return nil, fmt.Errorf("plist: real of %d bytes at %d", size, off)
		}
		switch size {
		case 4:
			return float64(math.Float32frombits(uint32(beUint(d.b[off+1 : off+5])))), nil
		case 8:
			return math.Float64frombits(beUint(d.b[off+1 : off+9])), nil
		}
		return nil, fmt.Errorf("plist: real of %d bytes at %d", size, off)
	case 0x3: // date: float64 seconds since 2001
		if marker != 0x33 || off+9 > limit {
			return nil, fmt.Errorf("plist: bad date at %d", off)
		}
		secs := math.Float64frombits(beUint(d.b[off+1 : off+9]))
		if math.IsNaN(secs) || math.IsInf(secs, 0) || math.Abs(secs) > 1e12 {
			return time.Time{}, nil
		}
		whole, frac := math.Modf(secs)
		return appleEpoch.Add(time.Duration(whole)*time.Second + time.Duration(frac*1e9)).UTC(), nil
	case 0x4: // data
		n, p, err := d.count(off)
		if err != nil {
			return nil, err
		}
		if p+n > limit {
			return nil, fmt.Errorf("plist: data of %d bytes at %d", n, off)
		}
		return append([]byte(nil), d.b[p:p+n]...), nil
	case 0x5: // ASCII string
		n, p, err := d.count(off)
		if err != nil {
			return nil, err
		}
		if p+n > limit {
			return nil, fmt.Errorf("plist: string of %d bytes at %d", n, off)
		}
		return string(d.b[p : p+n]), nil
	case 0x6: // UTF-16BE string
		n, p, err := d.count(off)
		if err != nil {
			return nil, err
		}
		if p+2*n > limit {
			return nil, fmt.Errorf("plist: utf-16 string of %d units at %d", n, off)
		}
		units := make([]uint16, n)
		for i := range units {
			units[i] = binary.BigEndian.Uint16(d.b[p+2*i:])
		}
		return string(utf16.Decode(units)), nil
	case 0x8: // UID, n+1 bytes
		size := int(marker&0x0f) + 1
		if off+1+size > limit {
			return nil, fmt.Errorf("plist: uid of %d bytes at %d", size, off)
		}
		return UID(beUint(d.b[off+1 : off+1+size])), nil
	case 0xa, 0xc: // array, set
		n, p, err := d.count(off)
		if err != nil {
			return nil, err
		}
		if p+n*d.refSize > limit {
			return nil, fmt.Errorf("plist: array of %d at %d", n, off)
		}
		if d.visiting[ref] {
			return nil, errors.New("plist: reference cycle")
		}
		d.visiting[ref] = true
		defer delete(d.visiting, ref)
		out := make([]any, 0, n)
		for i := 0; i < n; i++ {
			child := beUint(d.b[p+i*d.refSize : p+(i+1)*d.refSize])
			v, err := d.object(child, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case 0xd: // dict: key refs then value refs
		n, p, err := d.count(off)
		if err != nil {
			return nil, err
		}
		if p+2*n*d.refSize > limit {
			return nil, fmt.Errorf("plist: dict of %d at %d", n, off)
		}
		if d.visiting[ref] {
			return nil, errors.New("plist: reference cycle")
		}
		d.visiting[ref] = true
		defer delete(d.visiting, ref)
		out := make(map[string]any, n)
		for i := 0; i < n; i++ {
			kref := beUint(d.b[p+i*d.refSize : p+(i+1)*d.refSize])
			vref := beUint(d.b[p+(n+i)*d.refSize : p+(n+i+1)*d.refSize])
			k, err := d.object(kref, depth+1)
			if err != nil {
				return nil, err
			}
			ks, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("plist: dict key of type %T", k)
			}
			v, err := d.object(vref, depth+1)
			if err != nil {
				return nil, err
			}
			out[ks] = v
		}
		return out, nil
	}
	return nil, fmt.Errorf("plist: unknown marker %#x at %d", marker, off)
}

// ---- XML ---------------------------------------------------------------------------

// utf16ToUTF8 transcodes a UTF-16 byte stream (big- or little-endian).
func utf16ToUTF8(b []byte, bigEndian bool) []byte {
	units := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		if bigEndian {
			units = append(units, binary.BigEndian.Uint16(b[i:]))
		} else {
			units = append(units, binary.LittleEndian.Uint16(b[i:]))
		}
	}
	return []byte(string(utf16.Decode(units)))
}

// charsetReader converts the encodings a plist's XML declaration may name:
// UTF-8 (and ASCII) as is, UTF-16 transcoded; anything else is refused
// with a clear error rather than misread.
func charsetReader(charset string, in io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "", "utf-8", "utf8", "us-ascii", "ascii":
		return in, nil
	case "utf-16", "utf-16le", "utf-16be":
		raw, err := io.ReadAll(in)
		if err != nil {
			return nil, err
		}
		be := strings.HasSuffix(strings.ToLower(charset), "be")
		if len(raw) >= 2 && raw[0] == 0xfe && raw[1] == 0xff {
			be, raw = true, raw[2:]
		} else if len(raw) >= 2 && raw[0] == 0xff && raw[1] == 0xfe {
			be, raw = false, raw[2:]
		}
		return bytes.NewReader(utf16ToUTF8(raw, be)), nil
	}
	return nil, fmt.Errorf("plist: xml encoding %q is not supported (UTF-8 and UTF-16 are)", charset)
}

// DecodeXML parses the XML form: one value under <plist>. A UTF-16 file
// (a byte-order mark, or an encoding declaration) is transcoded first.
func DecodeXML(b []byte) (any, error) {
	if len(b) >= 2 && ((b[0] == 0xfe && b[1] == 0xff) || (b[0] == 0xff && b[1] == 0xfe)) {
		b = utf16ToUTF8(b[2:], b[0] == 0xfe)
		// the declaration inside now names an encoding the bytes no longer have
		b = bytes.Replace(b, []byte(`encoding="UTF-16"`), []byte(`encoding="UTF-8"`), 1)
		b = bytes.Replace(b, []byte(`encoding="utf-16"`), []byte(`encoding="UTF-8"`), 1)
	}
	dec := xml.NewDecoder(bytes.NewReader(b))
	dec.Strict = false
	dec.CharsetReader = charsetReader
	var top any
	got := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("plist: xml: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "plist" {
			continue
		}
		if got {
			return nil, errors.New("plist: more than one top-level value")
		}
		v, err := xmlValue(dec, se, 0)
		if err != nil {
			return nil, err
		}
		top, got = v, true
	}
	if !got {
		return nil, errors.New("plist: no value (not a property list)")
	}
	return top, nil
}

// xmlText reads the character data up to the element's end tag.
func xmlText(dec *xml.Decoder, se xml.StartElement) (string, error) {
	var sb strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", fmt.Errorf("plist: xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.CharData:
			sb.Write(t)
		case xml.EndElement:
			return sb.String(), nil
		case xml.StartElement:
			return "", fmt.Errorf("plist: xml: <%s> inside <%s>", t.Name.Local, se.Name.Local)
		}
	}
}

func xmlValue(dec *xml.Decoder, se xml.StartElement, depth int) (any, error) {
	if depth > maxDepth {
		return nil, errors.New("plist: nesting too deep")
	}
	switch se.Name.Local {
	case "string", "key":
		return xmlText(dec, se)
	case "integer":
		s, err := xmlText(dec, se)
		if err != nil {
			return nil, err
		}
		s = strings.TrimSpace(s)
		if v, err := strconv.ParseInt(s, 0, 64); err == nil {
			return v, nil
		}
		if v, err := strconv.ParseUint(s, 0, 64); err == nil {
			return int64(v), nil
		}
		return nil, fmt.Errorf("plist: integer %q", s)
	case "real":
		s, err := xmlText(dec, se)
		if err != nil {
			return nil, err
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return nil, fmt.Errorf("plist: real %q", s)
		}
		return v, nil
	case "true", "false":
		if _, err := xmlText(dec, se); err != nil {
			return nil, err
		}
		return se.Name.Local == "true", nil
	case "date":
		s, err := xmlText(dec, se)
		if err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
		if err != nil {
			return nil, fmt.Errorf("plist: date %q", s)
		}
		return t.UTC(), nil
	case "data":
		s, err := xmlText(dec, se)
		if err != nil {
			return nil, err
		}
		s = strings.Map(func(r rune) rune {
			if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
				return -1
			}
			return r
		}, s)
		v, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("plist: data: %w", err)
		}
		return v, nil
	case "array":
		var out []any
		for {
			tok, err := dec.Token()
			if err != nil {
				return nil, fmt.Errorf("plist: xml: %w", err)
			}
			switch t := tok.(type) {
			case xml.StartElement:
				v, err := xmlValue(dec, t, depth+1)
				if err != nil {
					return nil, err
				}
				out = append(out, v)
			case xml.EndElement:
				if out == nil {
					out = []any{}
				}
				return out, nil
			}
		}
	case "dict":
		out := map[string]any{}
		key, haveKey := "", false
		for {
			tok, err := dec.Token()
			if err != nil {
				return nil, fmt.Errorf("plist: xml: %w", err)
			}
			switch t := tok.(type) {
			case xml.StartElement:
				if t.Name.Local == "key" {
					k, err := xmlText(dec, t)
					if err != nil {
						return nil, err
					}
					key, haveKey = k, true
					continue
				}
				if !haveKey {
					return nil, fmt.Errorf("plist: xml: <%s> in a dict without a key", t.Name.Local)
				}
				v, err := xmlValue(dec, t, depth+1)
				if err != nil {
					return nil, err
				}
				out[key] = v
				haveKey = false
			case xml.EndElement:
				return out, nil
			}
		}
	}
	return nil, fmt.Errorf("plist: xml: unknown element <%s>", se.Name.Local)
}

// ---- accessors ---------------------------------------------------------------------

// Dict returns v as a dict, nil for anything else.
func Dict(v any) map[string]any {
	d, _ := v.(map[string]any)
	return d
}

// Array returns v as an array; a scalar becomes a one-element array, nil
// stays nil.
func Array(v any) []any {
	switch x := v.(type) {
	case nil:
		return nil
	case []any:
		return x
	}
	return []any{v}
}

// Strings renders v as strings: a string is itself, an array its elements
// (scalars rendered, nested values JSON-rendered), nil is nil.
func Strings(v any) []string {
	arr := Array(v)
	if arr == nil {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		out = append(out, Render(e))
	}
	return out
}

// String is the first of Strings, "" when absent.
func String(v any) string {
	if s := Strings(v); len(s) > 0 {
		return s[0]
	}
	return ""
}

// Int reads an integer: int64, an integral float, a numeric string, or the
// first element of an array of those.
func Int(v any) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1<<62 {
			return int64(x), true
		}
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return n, err == nil
	case []any:
		if len(x) > 0 {
			return Int(x[0])
		}
	}
	return 0, false
}

// Float reads a number: float64, int64, a numeric string, or the first of
// an array of those.
func Float(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int64:
		return float64(x), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	case []any:
		if len(x) > 0 {
			return Float(x[0])
		}
	}
	return 0, false
}

// Bool reads a boolean: bool, an integer 0/1, the strings true/false/yes/no,
// or the first of an array of those.
func Bool(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case int64:
		return x != 0, true
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true", "yes", "1":
			return true, true
		case "false", "no", "0":
			return false, true
		}
	case []any:
		if len(x) > 0 {
			return Bool(x[0])
		}
	}
	return false, false
}

// Time reads a date: time.Time, or seconds since the Unix epoch as a
// number (the shape accountPolicyData uses), or the first of an array.
func Time(v any) (time.Time, bool) {
	switch x := v.(type) {
	case time.Time:
		return x, !x.IsZero()
	case float64, int64, string:
		if f, ok := Float(x); ok && f > 0 {
			whole, frac := math.Modf(f)
			return time.Unix(int64(whole), int64(frac*1e9)).UTC(), true
		}
	case []any:
		if len(x) > 0 {
			return Time(x[0])
		}
	}
	return time.Time{}, false
}

// Render writes a value verbatim as compact JSON — a string stays bare —
// for record fields that carry a plist value whole (KeepAlive's dict,
// StartCalendarInterval). Bytes render base64, dates RFC 3339.
func Render(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	case UID:
		return "UID(" + strconv.FormatUint(uint64(x), 10) + ")"
	}
	b, err := json.Marshal(jsonable(v))
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// jsonable rewrites the values encoding/json cannot render as themselves.
func jsonable(v any) any {
	switch x := v.(type) {
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	case UID:
		return "UID(" + strconv.FormatUint(uint64(x), 10) + ")"
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = jsonable(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = jsonable(e)
		}
		return out
	}
	return v
}
