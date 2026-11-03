package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestListenWorkspaceReusesOccupiedPort(t *testing.T) {
	existing := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "document still available")
	}))
	existing.Listener.Close()
	var err error
	existing.Listener, err = net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	existing.Start()
	defer existing.Close()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(existing.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	var opened []string
	listener, err := listenWorkspace(port, &output, func(target string) { opened = append(opened, target) })
	if listener != nil {
		listener.Close()
		t.Fatal("second launch unexpectedly acquired the port")
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(opened) != 1 || opened[0] != "http://localhost:"+port+"/" {
		t.Fatalf("opened = %v", opened)
	}
	response, err := existing.Client().Get(opened[0])
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "document still available" {
		t.Fatalf("existing server response = %q, %v", body, err)
	}
}

func TestListenWorkspaceNewAndInvalidPorts(t *testing.T) {
	var output bytes.Buffer
	open := func(target string) { t.Fatalf("unexpected browser launch: %s", target) }
	listener, err := listenWorkspace("0", &output, open)
	if err != nil {
		t.Fatal(err)
	}
	if listener == nil || listener.Addr().(*net.TCPAddr).Port == 0 {
		t.Fatal("no listening port assigned")
	}
	listener.Close()
	if listener, err = listenWorkspace("65536", &output, open); listener != nil || err == nil {
		t.Fatalf("invalid port = %v, %v", listener, err)
	}
}

func TestRepeatedStartupLeavesWorkspaceUntouched(t *testing.T) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	for _, command := range []string{"", "serve"} {
		t.Run("command="+command, func(t *testing.T) {
			home := t.TempDir()
			workspace := filepath.Join(home, ".mdoc")
			if command == "serve" {
				if err := os.Mkdir(workspace, 0700); err != nil {
					t.Fatal(err)
				}
				for name, content := range map[string]string{
					"workspace.json": `{"activeDatabase":"research.db"}`,
					"research.db":    "must not be opened as SQLite",
				} {
					if err := os.WriteFile(filepath.Join(workspace, name), []byte(content), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWorkspaceStartupProcess$")
			child.Env = append(os.Environ(),
				"MDOC_STARTUP_TEST_CHILD=1", "MDOC_STARTUP_TEST_COMMAND="+command,
				"HOME="+home, "USERPROFILE="+home, "MDOC_NO_BROWSER=1",
				"API_PORT="+port,
			)
			output, err := child.CombinedOutput()
			if err != nil {
				t.Fatalf("repeated launch = %v\n%s", err, output)
			}
			if strings.Contains(string(output), "bind:") || !strings.Contains(string(output), "http://localhost:"+port+"/") {
				t.Fatalf("unexpected startup output: %s", output)
			}
			if command == "" {
				if _, err := os.Stat(workspace); !os.IsNotExist(err) {
					t.Fatalf("second launch created a workspace: %v", err)
				}
				return
			}
			entries, err := os.ReadDir(workspace)
			if err != nil || len(entries) != 2 {
				t.Fatalf("workspace files changed: %v, %v", entries, err)
			}
			for name, content := range map[string]string{
				"workspace.json": `{"activeDatabase":"research.db"}`,
				"research.db":    "must not be opened as SQLite",
			} {
				actual, err := os.ReadFile(filepath.Join(workspace, name))
				if err != nil || string(actual) != content {
					t.Fatalf("%s changed: %q, %v", name, actual, err)
				}
			}
		})
	}
}

func TestWorkspaceStartupProcess(t *testing.T) {
	if os.Getenv("MDOC_STARTUP_TEST_CHILD") != "1" {
		return
	}
	os.Args = []string{"mdoc"}
	if command := os.Getenv("MDOC_STARTUP_TEST_COMMAND"); command != "" {
		os.Args = append(os.Args, command)
	}
	main()
}
