package sqlite

import "syscall"

func detectFS(dir string) (FSInfo, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return FSInfo{}, err
	}
	name := make([]byte, 0, len(stat.Fstypename))
	for _, c := range stat.Fstypename {
		if c == 0 {
			break
		}
		name = append(name, byte(c))
	}
	return classifyDarwin(string(name)), nil
}
