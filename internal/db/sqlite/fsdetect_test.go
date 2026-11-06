package sqlite

import "testing"

func TestClassifyLinux(t *testing.T) {
	tests := []struct {
		magic uint32
		want  FSInfo
	}{
		{0x6969, FSInfo{"nfs", true}},
		{0x517B, FSInfo{"smb", true}},
		{0xFF534D42, FSInfo{"cifs", true}},
		{0xFE534D42, FSInfo{"smb2", true}},
		{0x0BD00BD0, FSInfo{"lustre", true}},
		{0x47504653, FSInfo{"gpfs", true}},
		{0x65735546, FSInfo{"fuse", true}},
		{0x5346414F, FSInfo{"afs", true}},
		{0x6B414653, FSInfo{"afs", true}},
		{0x01021997, FSInfo{"9p", true}},
		{0xEF53, FSInfo{"0xef53", false}},
	}
	for _, tt := range tests {
		t.Run(tt.want.Name, func(t *testing.T) {
			if got := classifyLinux(tt.magic); got != tt.want {
				t.Errorf("classifyLinux(%#x) = %+v, want %+v", tt.magic, got, tt.want)
			}
		})
	}
}

func TestClassifyDarwin(t *testing.T) {
	tests := []struct {
		name string
		want FSInfo
	}{
		{"nfs", FSInfo{"nfs", true}},
		{"smbfs", FSInfo{"smbfs", true}},
		{"afpfs", FSInfo{"afpfs", true}},
		{"webdav", FSInfo{"webdav", true}},
		{"macfuse", FSInfo{"macfuse", true}},
		{"osxfuse", FSInfo{"osxfuse", true}},
		{"apfs", FSInfo{"apfs", false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyDarwin(tt.name); got != tt.want {
				t.Errorf("classifyDarwin(%q) = %+v, want %+v", tt.name, got, tt.want)
			}
		})
	}
}

func TestDetectRealFilesystem(t *testing.T) {
	info, err := detectFS(t.TempDir())
	if err != nil {
		t.Fatalf("detectFS: %v", err)
	}
	if info.Name == "" {
		t.Error("filesystem name is empty")
	}
}
