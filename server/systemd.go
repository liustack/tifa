package server

import (
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
)

// listenFDsStart is the first file descriptor of the sockets systemd passes, SD_LISTEN_FDS_START.
// Tests move it.
var listenFDsStart = 3

// The names a socket unit gives its sockets with FileDescriptorName, for the setting each serves.
const (
	socketAddr     = "server.addr"
	socketHTTPAddr = "server.http_addr"
)

// systemdSockets returns the listening sockets systemd passed to the program with socket
// activation, by the setting each serves, as sd_listen_fds reads them: LISTEN_PID names this
// process, LISTEN_FDS counts the sockets from the file descriptor first on, and LISTEN_FDNAMES
// holds their names, separated by colons. It returns nil when LISTEN_PID names another process
// or none, as when no systemd started the program.
//
// A socket named server.addr or server.http_addr serves that setting. A single socket with any
// other name, which is the name of its unit unless FileDescriptorName says otherwise, serves
// server.addr. Several sockets must be named server.addr and server.http_addr.
func systemdSockets(getenv func(string) string, pid, first int) (map[string]net.Listener, error) {
	if getenv("LISTEN_PID") != strconv.Itoa(pid) {
		return nil, nil
	}
	n, err := strconv.Atoi(getenv("LISTEN_FDS"))
	if err != nil || n < 1 {
		return nil, fmt.Errorf("tifa: LISTEN_FDS is %q, not a count of the sockets systemd passed", getenv("LISTEN_FDS"))
	}
	names := strings.Split(getenv("LISTEN_FDNAMES"), ":")
	if len(names) != n {
		names = make([]string, n)
	}
	keys := make([]string, n)
	for i, name := range names {
		switch {
		case name == socketAddr || name == socketHTTPAddr:
			keys[i] = name
		case n == 1:
			keys[i] = socketAddr
		default:
			return nil, fmt.Errorf("tifa: systemd passed %d sockets named %q: name each with FileDescriptorName=%s or %s in its socket unit", n, getenv("LISTEN_FDNAMES"), socketAddr, socketHTTPAddr)
		}
		if slices.Contains(keys[:i], keys[i]) {
			return nil, fmt.Errorf("tifa: systemd passed two sockets for %s", keys[i])
		}
	}
	sockets := map[string]net.Listener{}
	for i, key := range keys {
		f := os.NewFile(uintptr(first+i), key)
		ln, err := net.FileListener(f) // a copy that closes on exec, so f goes
		f.Close()
		if err != nil {
			closeAll(sockets)
			return nil, fmt.Errorf("tifa: the socket systemd passed for %s: %w", key, err)
		}
		sockets[key] = ln
	}
	return sockets, nil
}

// takeSystemdSockets reads the sockets systemd passed to this process and removes the
// variables that name them, so the programs it starts do not take them for their own.
func takeSystemdSockets() (map[string]net.Listener, error) {
	sockets, err := systemdSockets(os.Getenv, os.Getpid(), listenFDsStart)
	if sockets != nil || err != nil {
		for _, name := range []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"} {
			os.Unsetenv(name)
		}
	}
	return sockets, err
}

// listen returns the socket systemd passed for the setting key, or listens on addr.
func listen(sockets map[string]net.Listener, key, addr string) (ln net.Listener, systemd bool, err error) {
	if ln := sockets[key]; ln != nil {
		delete(sockets, key)
		return ln, true, nil
	}
	if ln, err = net.Listen("tcp", addr); err != nil {
		return nil, false, listenError(key, err)
	}
	return ln, false, nil
}

// unusedSocket fails when systemd passed a socket for server.http_addr and the settings serve
// none: it is served only with HTTPS on and server.http_addr not off.
func unusedSocket(sockets map[string]net.Listener, plainHTTP bool) error {
	if sockets[socketHTTPAddr] == nil || plainHTTP {
		return nil
	}
	return errors.New("tifa: systemd passed a socket for server.http_addr, which is served only with HTTPS on and server.http_addr not off")
}

// socketFrom is what the line that names an address adds when systemd passed its socket.
func socketFrom(systemd bool) []any {
	if systemd {
		return []any{"socket", "systemd"}
	}
	return nil
}

func closeAll(sockets map[string]net.Listener) {
	for key, ln := range sockets {
		ln.Close()
		delete(sockets, key)
	}
}
