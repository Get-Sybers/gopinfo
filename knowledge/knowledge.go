// Package knowledge is the image's knowledge store (docs/linux §4, Layer
// 1): what the Layer-1 parsers — gohost, gousers, gonetwork — extracted
// about the image itself, loaded back so the daemon parsers can be
// enriched with it. The plaso preprocessing analogue: uid→name from the
// image's own passwd, the host's identity and timezone, the volume
// mappings.
//
// The boundary (decision 14): this is image-SELF-knowledge, joined
// mechanically and fill-only — a resolved name appears BESIDE the native
// numeric value, never in its place, and the Host block states the
// context that was applied. Cross-record correlation, relationships and
// guids remain byakugan's.
package knowledge

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // zones resolve without relying on the host tzdata

	"github.com/get-sybers/gopinfo/record"
)

// Store is one image's knowledge, built from Layer-1 record files.
// A nil *Store is safe everywhere and enriches nothing.
type Store struct {
	Hostname  string
	MachineID string
	OS        string
	Timezone  string
	loc       *time.Location
	users     map[string]string // uid -> name
	groups    map[string]string // gid -> name
	mounts    map[string]string // fs uuid -> mount point
}

// Load walks dir for *.jsonl files (a Layer-1 OUT_DIR tree) and builds
// the store. A missing or empty dir returns nil — enrichment is simply
// absent, never an error.
func Load(dir string) *Store {
	if dir == "" {
		return nil
	}
	s := &Store{
		users:  map[string]string{},
		groups: map[string]string{},
		mounts: map[string]string{},
	}
	found := false
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		if s.loadFile(p) {
			found = true
		}
		return nil
	})
	if !found {
		return nil
	}
	if s.Timezone != "" {
		if l, err := time.LoadLocation(s.Timezone); err == nil {
			s.loc = l
		}
	}
	return s
}

type knowledgeRow struct {
	RecordType string `json:"RecordType"`
	Username   string `json:"Username"`
	UID        *int64 `json:"UID"`
	GroupName  string `json:"GroupName"`
	GID        *int64 `json:"GID"`
	Hostname   string `json:"Hostname"`
	MachineID  string `json:"MachineID"`
	Timezone   string `json:"Timezone"`
	PrettyName string `json:"PrettyName"`
	ID         string `json:"ID"`
	VersionID  string `json:"VersionID"`
	UUID       string `json:"UUID"`
	MountPoint string `json:"MountPoint"`
}

func (s *Store) loadFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	used := false
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r knowledgeRow
		if json.Unmarshal(line, &r) != nil {
			continue
		}
		switch r.RecordType {
		case "account":
			if r.UID != nil && r.Username != "" {
				uid := strconv.FormatInt(*r.UID, 10)
				if _, dup := s.users[uid]; !dup {
					s.users[uid] = r.Username
				}
				used = true
			}
		case "group":
			if r.GID != nil && r.GroupName != "" {
				gid := strconv.FormatInt(*r.GID, 10)
				if _, dup := s.groups[gid]; !dup {
					s.groups[gid] = r.GroupName
				}
				used = true
			}
		case "hostname":
			if s.Hostname == "" {
				s.Hostname = r.Hostname
			}
			used = true
		case "machine_id":
			if s.MachineID == "" {
				s.MachineID = r.MachineID
			}
			used = true
		case "timezone":
			if s.Timezone == "" && r.Timezone != "" {
				s.Timezone = r.Timezone
			}
			used = true
		case "os_release":
			if s.OS == "" {
				if r.PrettyName != "" {
					s.OS = r.PrettyName
				} else if r.ID != "" {
					s.OS = strings.TrimSpace(r.ID + " " + r.VersionID)
				}
			}
			used = true
		case "fstab_entry":
			if r.UUID != "" && r.MountPoint != "" {
				if _, dup := s.mounts[strings.ToLower(r.UUID)]; !dup {
					s.mounts[strings.ToLower(r.UUID)] = r.MountPoint
				}
				used = true
			}
		}
	}
	return used
}

// Username resolves a numeric uid string against the image's passwd;
// "" when unknown or the store is absent.
func (s *Store) Username(uid string) string {
	if s == nil {
		return ""
	}
	return s.users[uid]
}

// Groupname resolves a numeric gid string; "" when unknown.
func (s *Store) Groupname(gid string) string {
	if s == nil {
		return ""
	}
	return s.groups[gid]
}

// MountPoint resolves a filesystem UUID against the image's fstab;
// "" when unknown.
func (s *Store) MountPoint(uuid string) string {
	if s == nil {
		return ""
	}
	return s.mounts[strings.ToLower(uuid)]
}

// Location is the host's zone for interpreting naive timestamps; nil
// when the store is absent or the zone did not resolve (callers then
// keep the naive-as-UTC rule).
func (s *Store) Location() *time.Location {
	if s == nil {
		return nil
	}
	return s.loc
}

// Host is the record.Host context block this store stamps.
func (s *Store) Host() *record.Host {
	if s == nil {
		return nil
	}
	h := &record.Host{
		Hostname: s.Hostname, MachineID: s.MachineID,
		OS: s.OS, Timezone: s.Timezone,
	}
	if h.Hostname == "" && h.MachineID == "" && h.OS == "" && h.Timezone == "" {
		return nil
	}
	return h
}
