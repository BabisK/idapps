package cmd

import (
	"net"
	"os"
	"path/filepath"

	"github.com/Microsoft/go-winio"
)

func listenFakeEngine() (net.Listener, string, func(), error) {
	dir, err := os.MkdirTemp("", "idapps-api-")
	if err != nil {
		return nil, "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	name := filepath.Base(dir)
	listener, err := winio.ListenPipe(`\\.\pipe\`+name, nil)
	if err != nil {
		cleanup()
		return nil, "", nil, err
	}
	return listener, "npipe:////./pipe/" + name, cleanup, nil
}
