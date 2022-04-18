package app

import (
	stdlog "log"
	"os"
	"path/filepath"
)

var (
	// DefaultNodeHome default home directories for the application daemon
	DefaultNodeHome string
)

func init() {
	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		stdlog.Println("Failed to get home dir %2", err)
	}

	DefaultNodeHome = filepath.Join(userHomeDir, ".bitsongd")
}
