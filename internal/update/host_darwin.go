//go:build darwin

package update

import (
	"encoding/hex"
	"fmt"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"
)

func machineID() (string, error) {
	library, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return "", fmt.Errorf("load macOS system library: %w", err)
	}
	defer purego.Dlclose(library)
	symbol, err := purego.Dlsym(library, "gethostuuid")
	if err != nil {
		return "", fmt.Errorf("resolve macOS host UUID API: %w", err)
	}
	var gethostuuid func(*[16]byte, *unix.Timespec) int32
	purego.RegisterFunc(&gethostuuid, symbol)
	var uuid [16]byte
	timeout := unix.Timespec{Sec: 5}
	if gethostuuid(&uuid, &timeout) != 0 {
		return "", fmt.Errorf("get macOS host UUID failed or timed out")
	}
	if uuid == [16]byte{} {
		return "", fmt.Errorf("macOS returned an empty host UUID")
	}
	return hex.EncodeToString(uuid[:]), nil
}
