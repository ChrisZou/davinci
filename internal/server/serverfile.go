package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ServerFile is <data>/server.json: where the server holding that data
// directory listens. The port is not fixed (DefaultPort may be taken), so the
// CLI reads this rather than assuming one.
type ServerFile struct {
	URL  string `json:"url"`
	PID  int    `json:"pid"`
	Boot string `json:"boot"`
}

// ServerFilePath is where a data directory's server.json lives.
func ServerFilePath(dataDir string) string { return filepath.Join(dataDir, "server.json") }

// ReadServerFile reads a data directory's server.json. It may be stale — the
// server it names may have died — so callers check that it answers.
func ReadServerFile(dataDir string) (*ServerFile, error) {
	b, err := os.ReadFile(ServerFilePath(dataDir))
	if err != nil {
		return nil, err
	}
	var f ServerFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// writeServerFile replaces server.json in one step, so a reader never sees half
// of it.
func writeServerFile(dataDir string, f ServerFile) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	tmp := ServerFilePath(dataDir) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, ServerFilePath(dataDir))
}

// removeServerFile deletes server.json if it still names this server (boot):
// a newer server on the same data directory may have taken it over.
func removeServerFile(dataDir, boot string) {
	if f, err := ReadServerFile(dataDir); err == nil && f.Boot == boot {
		_ = os.Remove(ServerFilePath(dataDir))
	}
}

// lockDataDir claims the data directory for this process: one server per data
// directory, or two would edit the same documents behind each other's backs.
// A fixed port used to keep a second one out; now that a busy port only means
// "take another", this lock does. It is let go when the file is closed (or the
// process ends).
func lockDataDir(dataDir string) (*os.File, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dataDir, "server.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := tryLock(f); err != nil {
		f.Close()
		where := ""
		if sf, err := ReadServerFile(dataDir); err == nil {
			where = "（" + sf.URL + "）"
		}
		return nil, fmt.Errorf("数据目录 %s 已经有 davinci 服务在跑%s", dataDir, where)
	}
	return f, nil
}
