package plist

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

// pyBinary is Python plistlib's binary rendering of
//
//	{'Label':'x','RunAtLoad':True,'Off':False,'n':42,'f':1.5,
//	 'd':datetime(2024,1,2,3,4,5),'data':b'\x01\x02\x03','arr':['a','b',[1,2]],
//	 'unicode':'héllo ✓','neg':-7,'big':2**40,'nested':{'k':'v','empty':[]},
//	 'uid':UID(3)}
//
// — a reference encoder's output, not this package's.
const pyBinary = "62706c6973743030dd0102030405060708090a0b0c0d0e0f10111718191a1b1c1d2223554c6162656c534f66665952756e41744c6f61645361727253626967516454646174615166516e536e6567566e65737465645375696457756e69636f646551780809a312131451615162a21516100110021300000100000000003341c5a1da5280000043010203233ff8000000000000102a13fffffffffffffff9d21e1f202155656d707479516ba05176800367006800e9006c006c006f002027130823292d373b3f4146484a4e555961636465696b6d7072747d868a93959ea3a9abacaeb000000000000001010000000000000024000000000000000000000000000000bf"

func TestBinaryReference(t *testing.T) {
	b, _ := hex.DecodeString(pyBinary)
	v, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	d := Dict(v)
	if d == nil || len(d) != 13 {
		t.Fatalf("dict: %v", v)
	}
	if d["Label"] != "x" || d["RunAtLoad"] != true || d["Off"] != false {
		t.Fatalf("scalars: %v", d)
	}
	if d["n"] != int64(42) || d["neg"] != int64(-7) || d["big"] != int64(1<<40) || d["f"] != 1.5 {
		t.Fatalf("numbers: n=%v neg=%v big=%v f=%v", d["n"], d["neg"], d["big"], d["f"])
	}
	if dt, ok := d["d"].(time.Time); !ok || !dt.Equal(time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("date: %v", d["d"])
	}
	if data, ok := d["data"].([]byte); !ok || string(data) != "\x01\x02\x03" {
		t.Fatalf("data: %v", d["data"])
	}
	if d["unicode"] != "héllo ✓" {
		t.Fatalf("utf-16: %q", d["unicode"])
	}
	arr := Array(d["arr"])
	if len(arr) != 3 || arr[0] != "a" || len(Array(arr[2])) != 2 {
		t.Fatalf("array: %v", d["arr"])
	}
	nested := Dict(d["nested"])
	if nested["k"] != "v" || len(Array(nested["empty"])) != 0 || Array(nested["empty"]) == nil {
		t.Fatalf("nested: %v", d["nested"])
	}
	if d["uid"] != UID(3) {
		t.Fatalf("uid: %v", d["uid"])
	}
}

func TestBinaryCorruptionNeverPanics(t *testing.T) {
	b, _ := hex.DecodeString(pyBinary)
	for i := 8; i < len(b); i++ {
		m := append([]byte(nil), b...)
		m[i] ^= 0xff
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("byte %d: panic %v", i, r)
				}
			}()
			Decode(m)
		}()
	}
	for n := 0; n < len(b); n += 7 {
		if _, err := Decode(b[:n]); err == nil && n < len(b) {
			t.Fatalf("truncated to %d bytes decoded without error", n)
		}
	}
}

const xmlJob = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>org.keepassxc.KeePassXC</string>
	<key>ProgramArguments</key>
	<array>
		<string>/Applications/KeePassXC.app/Contents/MacOS/KeePassXC</string>
		<string>--minimized</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>StartInterval</key>
	<integer>3600</integer>
	<key>Nice</key>
	<integer>-5</integer>
	<key>Weight</key>
	<real>0.25</real>
	<key>Since</key>
	<date>2024-03-01T12:00:00Z</date>
	<key>Blob</key>
	<data>
	AQID
	</data>
	<key>StartCalendarInterval</key>
	<dict>
		<key>Hour</key>
		<integer>3</integer>
		<key>Minute</key>
		<integer>0</integer>
	</dict>
	<key>Empty</key>
	<array/>
</dict>
</plist>
`

func TestXML(t *testing.T) {
	v, err := Decode([]byte(xmlJob))
	if err != nil {
		t.Fatal(err)
	}
	d := Dict(v)
	if String(d["Label"]) != "org.keepassxc.KeePassXC" {
		t.Fatalf("label: %v", d["Label"])
	}
	if args := Strings(d["ProgramArguments"]); len(args) != 2 || args[1] != "--minimized" {
		t.Fatalf("args: %v", args)
	}
	if b, ok := Bool(d["RunAtLoad"]); !ok || !b {
		t.Fatalf("RunAtLoad: %v", d["RunAtLoad"])
	}
	if n, ok := Int(d["StartInterval"]); !ok || n != 3600 {
		t.Fatalf("StartInterval: %v", d["StartInterval"])
	}
	if n, _ := Int(d["Nice"]); n != -5 {
		t.Fatalf("Nice: %v", d["Nice"])
	}
	if f, _ := Float(d["Weight"]); f != 0.25 {
		t.Fatalf("Weight: %v", d["Weight"])
	}
	if ts, ok := Time(d["Since"]); !ok || ts.Year() != 2024 {
		t.Fatalf("Since: %v", d["Since"])
	}
	if b, ok := d["Blob"].([]byte); !ok || string(b) != "\x01\x02\x03" {
		t.Fatalf("Blob: %v", d["Blob"])
	}
	if got := Render(d["StartCalendarInterval"]); got != `{"Hour":3,"Minute":0}` {
		t.Fatalf("render: %s", got)
	}
	if e := Array(d["Empty"]); e == nil || len(e) != 0 {
		t.Fatalf("empty array: %v", d["Empty"])
	}
	if _, err := Decode([]byte("not a plist")); err == nil {
		t.Fatal("garbage must not decode")
	}
	if _, err := Decode([]byte("<plist><dict><string>no key</string></dict></plist>")); err == nil {
		t.Fatal("a dict value without a key must error")
	}
}

func TestAccessorsOverDslocalShapes(t *testing.T) {
	// dslocal stores every attribute as a one-element array of strings
	v, err := Decode([]byte(`<plist version="1.0"><dict>
		<key>uid</key><array><string>501</string></array>
		<key>name</key><array><string>gl</string><string>alias</string></array>
		<key>IsHidden</key><array><string>1</string></array>
		<key>passwordLastSetTime</key><real>1709288298.372935</real>
	</dict></plist>`))
	if err != nil {
		t.Fatal(err)
	}
	d := Dict(v)
	if n, ok := Int(d["uid"]); !ok || n != 501 {
		t.Fatalf("uid: %v", d["uid"])
	}
	if s := Strings(d["name"]); len(s) != 2 || s[0] != "gl" {
		t.Fatalf("name: %v", s)
	}
	if b, ok := Bool(d["IsHidden"]); !ok || !b {
		t.Fatalf("IsHidden: %v", d["IsHidden"])
	}
	if ts, ok := Time(d["passwordLastSetTime"]); !ok || !strings.HasPrefix(ts.Format(time.RFC3339), "2024-03-01T") {
		t.Fatalf("time: %v", ts)
	}
	if _, ok := Int(d["missing"]); ok {
		t.Fatal("a missing key must not read as an int")
	}
}

func TestXMLUTF16(t *testing.T) {
	src := "<?xml version=\"1.0\" encoding=\"UTF-16\"?><plist version=\"1.0\"><dict><key>Label</key><string>héllo</string></dict></plist>"
	for _, bigEndian := range []bool{true, false} {
		var b []byte
		if bigEndian {
			b = append(b, 0xfe, 0xff)
		} else {
			b = append(b, 0xff, 0xfe)
		}
		for _, u := range utf16.Encode([]rune(src)) {
			if bigEndian {
				b = append(b, byte(u>>8), byte(u))
			} else {
				b = append(b, byte(u), byte(u>>8))
			}
		}
		v, err := Decode(b)
		if err != nil {
			t.Fatalf("utf-16 be=%v: %v", bigEndian, err)
		}
		if String(Dict(v)["Label"]) != "héllo" {
			t.Fatalf("utf-16 be=%v: %v", bigEndian, v)
		}
	}
	if _, err := Decode([]byte(`<?xml version="1.0" encoding="ISO-8859-1"?><plist version="1.0"><dict/></plist>`)); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("an unsupported encoding must be refused clearly: %v", err)
	}
}
