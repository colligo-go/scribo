package sqlite

import "fmt"

type FSInfo struct {
	Name    string
	Network bool
}

var linuxNetworkFS = map[uint32]string{
	0x6969:     "nfs",
	0x517B:     "smb",
	0xFF534D42: "cifs",
	0xFE534D42: "smb2",
	0x0BD00BD0: "lustre",
	0x47504653: "gpfs",
	0x65735546: "fuse",
	0x5346414F: "afs",
	0x6B414653: "afs",
	0x01021997: "9p",
}

var darwinNetworkFS = map[string]bool{
	"nfs":     true,
	"smbfs":   true,
	"afpfs":   true,
	"webdav":  true,
	"macfuse": true,
	"osxfuse": true,
}

func classifyLinux(magic uint32) FSInfo {
	if name, ok := linuxNetworkFS[magic]; ok {
		return FSInfo{Name: name, Network: true}
	}
	return FSInfo{Name: fmt.Sprintf("0x%x", magic)}
}

func classifyDarwin(fstypename string) FSInfo {
	return FSInfo{Name: fstypename, Network: darwinNetworkFS[fstypename]}
}
