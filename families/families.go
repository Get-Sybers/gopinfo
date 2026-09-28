// Package families is the typed-event engine of the Linux matrix
// (docs/linux §4.1 rule 1): the high-value log families — sshd
// authentication, sudo command lines, pam session open/close, cron job
// lines — recognised ONCE, here, and applied wherever that event stream
// surfaces. On a systemd host the journal is the primary pathway (sshd,
// sudo and cron all log through it; the flat auth.log may not even
// exist), so gojournal and gosyslog both feed this engine: the same
// event yields the same RecordType and the same fields from either
// source, and byakugan's maps match on one shape.
package families

import (
	"regexp"
	"strconv"
	"strings"
)

// Typed carries the typed-family fields, embedded by every record shape
// that feeds the engine. Field names and JSON tags are part of the
// byakugan interface (the tools' contract.yml records sections).
type Typed struct {
	SSHEvent    string `json:"SSHEvent,omitempty"`
	Method      string `json:"Method,omitempty"`
	Username    string `json:"Username,omitempty"`
	TargetUser  string `json:"TargetUser,omitempty"`
	InvalidUser bool   `json:"InvalidUser,omitempty"`
	IPAddress   string `json:"IPAddress,omitempty"`
	Port        *int64 `json:"Port,omitempty"`
	KeyType     string `json:"KeyType,omitempty"`
	Fingerprint string `json:"Fingerprint,omitempty"`
	TTY         string `json:"TTY,omitempty"`
	PWD         string `json:"PWD,omitempty"`
	Command     string `json:"Command,omitempty"`
	PamModule   string `json:"PamModule,omitempty"`
	SessionOp   string `json:"SessionOp,omitempty"`
	ByUser      string `json:"ByUser,omitempty"`
	ByUID       *int64 `json:"ByUID,omitempty"`
}

var (
	sshAuthRe    = regexp.MustCompile(`^(Accepted|Failed) (\S+) for (invalid user )?(.+?) from (\S+) port (\d+)(?: ssh2)?(?::\s+(\S+)\s+(\S+))?\s*$`)
	sshInvalidRe = regexp.MustCompile(`^Invalid user (\S+) from (\S+)(?: port (\d+))?`)
	pamRe        = regexp.MustCompile(`^(pam_unix|pam_[a-z0-9_]+)\(([^)]+)\): session (opened|closed) for user ([^( ]+)(?:\(uid=(\d+)\))?(?: by (?:([^( ]+))?\(uid=(\d+)\))?`)
	sudoRe       = regexp.MustCompile(`^\s*(\S+) : (?:.*?;\s*)?TTY=(\S+)\s*;\s*PWD=(.*?)\s*;\s*USER=(\S+)\s*;\s*(?:ENV=\S+\s*;\s*)?COMMAND=(.*)$`)
	cronCmdRe    = regexp.MustCompile(`^\((\S+)\) CMD \((.*)\)\s*$`)
)

// Type inspects one log event — the syslog identifier (or journal
// SYSLOG_IDENTIFIER/_COMM) and the message — and, when it is one of the
// typed families, fills t and returns the family's RecordType; "" when
// the event is not a typed family. Values land verbatim; judging them is
// byakugan's.
func Type(ident, message string, t *Typed) string {
	switch {
	case ident == "sshd" || strings.HasPrefix(ident, "sshd"):
		if m := sshAuthRe.FindStringSubmatch(message); m != nil {
			if m[1] == "Accepted" {
				t.SSHEvent = "accepted"
			} else {
				t.SSHEvent = "failed"
			}
			t.Method = m[2]
			t.InvalidUser = m[3] != ""
			t.Username = m[4]
			t.IPAddress = m[5]
			if n, err := strconv.ParseInt(m[6], 10, 64); err == nil {
				t.Port = &n
			}
			t.KeyType, t.Fingerprint = m[7], m[8]
			return "sshd_event"
		}
		if m := sshInvalidRe.FindStringSubmatch(message); m != nil {
			t.SSHEvent = "invalid_user"
			t.InvalidUser = true
			t.Username = m[1]
			t.IPAddress = m[2]
			if m[3] != "" {
				if n, err := strconv.ParseInt(m[3], 10, 64); err == nil {
					t.Port = &n
				}
			}
			return "sshd_event"
		}
	case ident == "sudo":
		if m := sudoRe.FindStringSubmatch(message); m != nil {
			t.Username = m[1]
			t.TTY = m[2]
			t.PWD = m[3]
			t.TargetUser = m[4]
			t.Command = m[5]
			return "sudo_event"
		}
	case ident == "CRON" || ident == "crond" || ident == "cron":
		if m := cronCmdRe.FindStringSubmatch(message); m != nil {
			t.Username = m[1]
			t.Command = m[2]
			return "cron_event"
		}
	}
	if m := pamRe.FindStringSubmatch(message); m != nil {
		t.PamModule = m[1]
		t.SessionOp = m[3]
		t.Username = m[4]
		t.ByUser = m[6]
		uidStr := m[7]
		if uidStr == "" {
			uidStr = m[5]
		}
		if uidStr != "" {
			if n, err := strconv.ParseInt(uidStr, 10, 64); err == nil {
				t.ByUID = &n
			}
		}
		return "pam_session"
	}
	return ""
}
