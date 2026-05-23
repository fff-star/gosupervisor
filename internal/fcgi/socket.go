package fcgi

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"sync"
)

// SocketManager creates, binds, and manages a listening socket for fcgi programs.
// The socket outlives individual child processes and is closed only when the last
// child exits or the fcgi-program is explicitly stopped.
type SocketManager struct {
	listener    net.Listener
	socketAddr  string // original config value: "unix:///path" or "tcp://host:port"
	socketMode  uint32
	socketOwner string // "user:group"
	network     string // "unix" or "tcp"
	address     string // parsed address: "/path" or "host:port"

	mu       sync.Mutex
	refCount int
	closed   bool
}

// NewSocketManager creates a SocketManager from a socket URL.
func NewSocketManager(addr string, mode uint32, owner string) *SocketManager {
	return &SocketManager{
		socketAddr:  addr,
		socketMode:  mode,
		socketOwner: owner,
	}
}

// Listen parses the socket URL, creates the listener, and applies permissions.
func (sm *SocketManager) Listen() error {
	network, address, err := parseSocketAddr(sm.socketAddr)
	if err != nil {
		return err
	}
	sm.network = network
	sm.address = address

	listener, err := net.Listen(network, address)
	if err != nil {
		return fmt.Errorf("fcgi socket listen %s: %v", sm.socketAddr, err)
	}
	sm.listener = listener

	// Apply socket mode (Unix only)
	if network == "unix" && sm.socketMode > 0 {
		if err := os.Chmod(address, os.FileMode(sm.socketMode)); err != nil {
			listener.Close()
			return fmt.Errorf("fcgi socket chmod %o: %v", sm.socketMode, err)
		}
	}

	// Apply socket owner (Unix only)
	if network == "unix" && sm.socketOwner != "" {
		parts := strings.SplitN(sm.socketOwner, ":", 2)
		var uid, gid int
		u, err := user.Lookup(parts[0])
		if err != nil {
			uid, _ = strconv.Atoi(parts[0])
		} else {
			uid, _ = strconv.Atoi(u.Uid)
		}
		if len(parts) == 2 {
			g, err := user.LookupGroup(parts[1])
			if err != nil {
				gid, _ = strconv.Atoi(parts[1])
			} else {
				gid, _ = strconv.Atoi(g.Gid)
			}
		}
		if uid != 0 || gid != 0 {
			if err := os.Chown(address, uid, gid); err != nil {
				listener.Close()
				return fmt.Errorf("fcgi socket chown %s: %v", sm.socketOwner, err)
			}
		}
	}

	return nil
}

// Attach dups the listener fd and attaches it to the child command.
// The child receives the listening socket as fd 0 (stdin).
func (sm *SocketManager) Attach(cmd *exec.Cmd) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.closed || sm.listener == nil {
		return fmt.Errorf("fcgi socket not listening")
	}

	listenerFile, err := sm.listenerFile()
	if err != nil {
		return fmt.Errorf("fcgi socket file: %v", err)
	}

	cmd.ExtraFiles = []*os.File{listenerFile}

	originalCmd := strings.Join(quoteArgs(cmd.Args), " ")
	cmd.Args = []string{"/bin/sh", "-c",
		fmt.Sprintf("exec 0<&3 3<&-; exec %s", originalCmd)}

	sm.refCount++
	return nil
}

// Detach decrements the reference count. Returns true if this was the last reference.
func (sm *SocketManager) Detach() bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.refCount > 0 {
		sm.refCount--
	}
	return sm.refCount == 0
}

// RefCount returns the current number of attached children.
func (sm *SocketManager) RefCount() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.refCount
}

// Close closes the listener and removes the Unix socket file.
func (sm *SocketManager) Close() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.closed {
		return nil
	}
	sm.closed = true

	if sm.listener != nil {
		err := sm.listener.Close()
		sm.listener = nil
		if sm.network == "unix" {
			os.Remove(sm.address)
		}
		return err
	}
	return nil
}

// SocketAddr returns the original socket URL from the config.
func (sm *SocketManager) SocketAddr() string {
	return sm.socketAddr
}

// listenerFile extracts the underlying *os.File from the listener.
func (sm *SocketManager) listenerFile() (*os.File, error) {
	switch l := sm.listener.(type) {
	case *net.UnixListener:
		return l.File()
	case *net.TCPListener:
		return l.File()
	default:
		return nil, fmt.Errorf("unsupported listener type: %T", sm.listener)
	}
}

// parseSocketAddr parses a socket URL into network and address.
func parseSocketAddr(addr string) (network, address string, err error) {
	if strings.HasPrefix(addr, "unix://") {
		return "unix", strings.TrimPrefix(addr, "unix://"), nil
	}
	if strings.HasPrefix(addr, "tcp://") {
		return "tcp", strings.TrimPrefix(addr, "tcp://"), nil
	}
	return "", "", fmt.Errorf("invalid socket URL: %s (expected unix:// or tcp://)", addr)
}

// quoteArgs shell-escapes each argument for safe embedding in a shell command.
func quoteArgs(args []string) []string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellEscape(arg)
	}
	return quoted
}

// shellEscape wraps a string in single quotes, escaping any embedded single quotes.
func shellEscape(s string) string {
	if !strings.ContainsAny(s, "\"'\\$`!*?[](){}|&;<> \t\n") {
		return s
	}
	escaped := strings.ReplaceAll(s, "'", "'\\''")
	return "'" + escaped + "'"
}
