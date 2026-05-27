//go:build windows

package privilege

import "log/slog"

func DropFromEnv(_ string, _ *slog.Logger) error {
	return nil
}
