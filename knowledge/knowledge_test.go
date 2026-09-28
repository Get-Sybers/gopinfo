package knowledge

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadAndResolve(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "etc_passwd"), 0o755)
	os.WriteFile(filepath.Join(dir, "etc_passwd", "gousers.jsonl"), []byte(
		`{"RecordType":"account","Username":"root","UID":0}`+"\n"+
			`{"RecordType":"account","Username":"alice","UID":1000}`+"\n"+
			`{"RecordType":"group","GroupName":"sudo","GID":27}`+"\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "etc_hostname"), 0o755)
	os.WriteFile(filepath.Join(dir, "etc_hostname", "gohost.jsonl"), []byte(
		`{"RecordType":"hostname","Hostname":"web01"}`+"\n"+
			`{"RecordType":"machine_id","MachineID":"0123456789abcdef0123456789abcdef"}`+"\n"+
			`{"RecordType":"timezone","Timezone":"Europe/Berlin"}`+"\n"+
			`{"RecordType":"os_release","PrettyName":"Debian GNU/Linux 13 (trixie)"}`+"\n"+
			`{"RecordType":"fstab_entry","UUID":"9F8E7D6C-1a2b-3c4d-5e6f-708192a3b4c5","MountPoint":"/data"}`+"\n"), 0o644)

	s := Load(dir)
	if s == nil {
		t.Fatal("store not loaded")
	}
	if s.Username("1000") != "alice" || s.Username("0") != "root" || s.Username("9999") != "" {
		t.Fatalf("users: %+v", s.users)
	}
	if s.Groupname("27") != "sudo" {
		t.Fatalf("groups: %+v", s.groups)
	}
	if s.MountPoint("9f8e7d6c-1a2b-3c4d-5e6f-708192a3b4c5") != "/data" {
		t.Fatalf("mounts: %+v", s.mounts)
	}
	h := s.Host()
	if h == nil || h.Hostname != "web01" || h.OS != "Debian GNU/Linux 13 (trixie)" ||
		h.Timezone != "Europe/Berlin" {
		t.Fatalf("host: %+v", h)
	}
	loc := s.Location()
	if loc == nil {
		t.Fatal("zone did not resolve (tzdata embed missing?)")
	}
	// Berlin is UTC+1 in winter: a naive 12:00 local = 11:00Z
	tt := time.Date(2026, 1, 15, 12, 0, 0, 0, loc)
	if tt.UTC().Hour() != 11 {
		t.Fatalf("zone math: %v", tt.UTC())
	}
}

func TestAbsentStore(t *testing.T) {
	if Load("") != nil || Load(filepath.Join(t.TempDir(), "missing")) != nil {
		t.Fatal("absent store must be nil")
	}
	var s *Store
	if s.Username("0") != "" || s.Location() != nil || s.Host() != nil {
		t.Fatal("nil store must resolve nothing")
	}
}
