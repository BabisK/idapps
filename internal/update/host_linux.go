//go:build linux

package update

import (
	"os"
	"strings"
)

func machineID() (string, error) {
	b, e := os.ReadFile("/etc/machine-id")
	return strings.TrimSpace(string(b)), e
}
