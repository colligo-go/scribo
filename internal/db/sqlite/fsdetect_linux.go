package sqlite

import "syscall"

func detectFS(dir string) (FSInfo, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return FSInfo{}, err
	}
	return classifyLinux(uint32(stat.Type)), nil
}
