package discover

import (
	"bytes"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// a bzip2 stream of "hello wifi\n" (bzip2 -9), detected by content
const helloBz2 = "425a683931415926535999e68b200000025180001040000364808020002200cd420c988e6af10c83c5dc914e14242679a2c800"

func TestOpenAutoBzip2(t *testing.T) {
	b, err := hex.DecodeString(helloBz2)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "wifi.log.0.bz2")
	os.WriteFile(p, b, 0o644)
	f, err := OpenAuto(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := io.ReadAll(f)
	if err != nil || !bytes.Equal(got, []byte("hello wifi\n")) {
		t.Fatalf("%q %v", got, err)
	}
	for _, name := range []string{"wifi.log.0.bz2", "wifi.log.bz2", "wifi.log.1", "wifi.log.10.gz"} {
		if !Rotated(name, "wifi.log") {
			t.Errorf("%s not a rotation", name)
		}
	}
	if Rotated("wifi.log.conf", "wifi.log") {
		t.Error("wifi.log.conf claimed")
	}
}
