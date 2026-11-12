//go:build !windows

package cmd

import (
	"net"
	"os"
	"path/filepath"
)

func listenFakeEngine() (net.Listener, string, func(), error) {
	dir, err := os.MkdirTemp("", "idapps-api-")
	if err != nil {
		return nil, "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	socket := filepath.Join(dir, "docker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		cleanup()
		return nil, "", nil, err
	}
	return listener, "unix://" + socket, cleanup, nil
}
