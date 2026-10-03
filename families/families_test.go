package families

import "testing"

func TestSSHAccepted(t *testing.T) {
	var f Typed
	fp := "SHA256:AbCdEf0123456789AbCdEf0123456789AbCdEf01234"
	rt := Type("sshd", "Accepted publickey for alice from 198.51.100.7 port 51234 ssh2: ED25519 "+fp, &f)
	if rt != "sshd_event" || f.SSHEvent != "accepted" || f.Method != "publickey" ||
		f.Username != "alice" || f.IPAddress != "198.51.100.7" || *f.Port != 51234 ||
		f.KeyType != "ED25519" || f.Fingerprint != fp || f.InvalidUser {
		t.Fatalf("accepted: %q %+v", rt, f)
	}
}

func TestSudoCronPam(t *testing.T) {
	var f Typed
	rt := Type("sudo", "   alice : TTY=pts/0 ; PWD=/home/alice ; USER=root ; COMMAND=/usr/bin/systemctl status nginx", &f)
	if rt != "sudo_event" || f.Username != "alice" || f.TargetUser != "root" ||
		f.TTY != "pts/0" || f.Command != "/usr/bin/systemctl status nginx" {
		t.Fatalf("sudo: %q %+v", rt, f)
	}

	f = Typed{}
	rt = Type("CRON", "(alice) CMD (/home/alice/bin/sync.sh)", &f)
	if rt != "cron_event" || f.Username != "alice" || f.Command != "/home/alice/bin/sync.sh" {
		t.Fatalf("cron: %q %+v", rt, f)
	}

	f = Typed{}
	rt = Type("sudo", "pam_unix(sudo:session): session opened for user root(uid=0) by alice(uid=1000)", &f)
	if rt != "pam_session" || f.PamModule != "pam_unix" || f.SessionOp != "opened" ||
		f.Username != "root" || f.ByUser != "alice" || *f.ByUID != 1000 {
		t.Fatalf("pam: %q %+v", rt, f)
	}
}

func TestUntypedStaysEmpty(t *testing.T) {
	var f Typed
	if rt := Type("systemd", "Started daily apt activities.", &f); rt != "" {
		t.Fatalf("untyped got %q", rt)
	}
	if rt := Type("sshd", "Server listening on 0.0.0.0 port 22.", &f); rt != "" {
		t.Fatalf("listening line got %q", rt)
	}
}
