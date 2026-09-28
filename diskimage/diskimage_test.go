package diskimage

import "testing"

func TestIsImageItem(t *testing.T) {
	cases := map[string]bool{
		"/in/disk.e01":       true,
		"/in/image.vmdk":     true,
		"/in/disk-flat.vmdk": false, // a VMDK extent is part of another item
		"/in/set.e02":        false, // an EWF continuation segment
		"/in/notes.txt":      false,
	}
	for path, want := range cases {
		if got := IsImageItem(path); got != want {
			t.Errorf("IsImageItem(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestImageItemName(t *testing.T) {
	if got := ImageItemName("/in", "/in/VM files/disk 1.vmdk"); got != "VM_files_disk_1.vmdk" {
		t.Errorf("ImageItemName = %q, want VM_files_disk_1.vmdk", got)
	}
	if got := ImageItemName("/in", "/in/plain.e01"); got != "plain.e01" {
		t.Errorf("ImageItemName = %q, want plain.e01", got)
	}
}
