//go:build !windows

package privilege

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

func DropFromEnv(path string, logger *slog.Logger) error {
	if os.Geteuid() != 0 || os.Getegid() != 0 {
		return nil
	}
	uid, err := envInt("PUID", 1000)
	if err != nil {
		return err
	}
	gid, err := envInt("PGID", 1000)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	logger.Info("setting data directory ownership", "path", path, "uid", uid, "gid", gid)
	if err := filepath.WalkDir(path, func(name string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		return os.Lchown(name, uid, gid)
	}); err != nil {
		return err
	}
	logger.Info("dropping privileges", "uid", uid, "gid", gid)
	if err := syscall.Setgroups([]int{}); err != nil {
		return err
	}
	if err := syscall.Setgid(gid); err != nil {
		return err
	}
	if err := syscall.Setuid(uid); err != nil {
		return err
	}
	return nil
}

func envInt(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return parsed, nil
}
