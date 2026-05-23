package fcgi

import (
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestParseSocketAddrUnix(t *testing.T) {
	network, address, err := parseSocketAddr("unix:///tmp/test.sock")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if network != "unix" {
		t.Errorf("expected network=unix, got %s", network)
	}
	if address != "/tmp/test.sock" {
		t.Errorf("expected address=/tmp/test.sock, got %s", address)
	}
}

func TestParseSocketAddrTCP(t *testing.T) {
	network, address, err := parseSocketAddr("tcp://localhost:9002")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if network != "tcp" {
		t.Errorf("expected network=tcp, got %s", network)
	}
	if address != "localhost:9002" {
		t.Errorf("expected address=localhost:9002, got %s", address)
	}
}

func TestParseSocketAddrInvalid(t *testing.T) {
	_, _, err := parseSocketAddr("invalid://foo")
	if err == nil {
		t.Fatal("expected error for invalid scheme")
	}
}

func TestSocketManagerListenUnix(t *testing.T) {
	dir := t.TempDir()
	sockPath := dir + "/test.sock"

	sm := NewSocketManager("unix://"+sockPath, 0700, "")
	if err := sm.Listen(); err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}
	defer sm.Close()

	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		t.Fatal("socket file was not created")
	}

	if sm.RefCount() != 0 {
		t.Errorf("expected RefCount=0, got %d", sm.RefCount())
	}
}

func TestSocketManagerListenTCP(t *testing.T) {
	sm := NewSocketManager("tcp://127.0.0.1:0", 0, "")
	if err := sm.Listen(); err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}
	defer sm.Close()

	_, port, err := net.SplitHostPort(sm.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if port == "0" {
		t.Fatal("expected non-zero port")
	}
}

func TestSocketManagerAttachDetach(t *testing.T) {
	dir := t.TempDir()
	sockPath := dir + "/test.sock"

	sm := NewSocketManager("unix://"+sockPath, 0700, "")
	if err := sm.Listen(); err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}
	defer sm.Close()

	cmd := exec.Command("true")
	if err := sm.Attach(cmd); err != nil {
		t.Fatalf("Attach() failed: %v", err)
	}
	if sm.RefCount() != 1 {
		t.Errorf("expected RefCount=1, got %d", sm.RefCount())
	}

	if len(cmd.ExtraFiles) != 1 {
		t.Fatalf("expected 1 ExtraFile, got %d", len(cmd.ExtraFiles))
	}

	if len(cmd.Args) < 3 || cmd.Args[0] != "/bin/sh" {
		t.Errorf("expected /bin/sh wrapper, got args=%v", cmd.Args)
	}

	wrapper := strings.Join(cmd.Args, " ")
	if !strings.Contains(wrapper, "0<&3") {
		t.Errorf("expected fd redirection in wrapper: %s", wrapper)
	}

	last := sm.Detach()
	if !last {
		t.Error("expected Detach() to return true (last reference)")
	}
	if sm.RefCount() != 0 {
		t.Errorf("expected RefCount=0, got %d", sm.RefCount())
	}
}

func TestSocketManagerClose(t *testing.T) {
	dir := t.TempDir()
	sockPath := dir + "/test.sock"

	sm := NewSocketManager("unix://"+sockPath, 0700, "")
	if err := sm.Listen(); err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}

	if err := sm.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Error("socket file still exists after Close()")
	}
}

func TestSocketManagerDoubleClose(t *testing.T) {
	dir := t.TempDir()
	sm := NewSocketManager("unix://"+dir+"/test.sock", 0700, "")
	if err := sm.Listen(); err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}
	if err := sm.Close(); err != nil {
		t.Fatalf("first Close() failed: %v", err)
	}
	if err := sm.Close(); err != nil {
		t.Fatalf("second Close() should be no-op: %v", err)
	}
}

func TestShellEscape(t *testing.T) {
	tests := []struct {
		input    string
		contains string
	}{
		{"hello", "hello"},
		{"hello world", "'"},
		{"/usr/bin/php-fpm", "/usr/bin/php-fpm"},
		{"it's working", "'"},
	}
	for _, tc := range tests {
		result := shellEscape(tc.input)
		if !strings.Contains(result, tc.contains) {
			t.Errorf("shellEscape(%q) = %q, expected to contain %q", tc.input, result, tc.contains)
		}
	}
}
