package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// recordingSSHEnv makes the test binary an ssh that writes its arguments to args.json and its input
// to stdin in the folder the variable names, see recordSSH.
const recordingSSHEnv = "TIFA_TEST_RECORDING_SSH"

// recordSSH is the ssh of recordingSSHEnv. It exits.
func recordSSH(dir string) {
	args, _ := json.Marshal(os.Args[1:])
	in, err := io.ReadAll(os.Stdin)
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "args.json"), args, 0o644)
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "stdin"), in, 0o644)
	}
	if err != nil {
		os.Stderr.WriteString(err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}

// useRecordingSSH makes the deploy commands run the recording ssh, and returns the folder it
// writes to.
func useRecordingSSH(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(recordingSSHEnv, dir)
	old := sshProgram
	sshProgram = os.Args[0]
	t.Cleanup(func() { sshProgram = old })
	return dir
}

// The server gets the script exactly as written, whatever it holds, on every system: on Windows
// the command line of ssh is one string that Go quotes and ssh splits again.
func TestDeploySSHPassesTheScriptUnchanged(t *testing.T) {
	dir := useRecordingSSH(t)
	script := "set -eu\nprintf '%s\\n' \"$HOME\" 'it''s' \"a \\\"quote\\\"\"\n\techo %PATH% ^& | > < `id` C:\\dir\\ \\\\\\\n" +
		"echo \"ends with a backslash\\\\\"\nécho ünïcode\r\nlast line\\"
	tgt := deployTarget{host: "vps.example.com"}
	if err := tgt.ssh(context.Background(), deployUser, script, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "args.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	want := []string{"-l", deployUser, "--", "vps.example.com", "sh -c " + shellQuote(script)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ssh got the arguments\n%q\nwant\n%q", got, want)
	}
}

// The upload sends the program through the input of ssh byte for byte, gzipped, and the script
// carries its SHA-256 sum, which the server checks. Bytes that a text mode would change, such as
// CRLF and Ctrl-Z, arrive as they are.
func TestDeploySSHUploadsTheProgramUnchanged(t *testing.T) {
	dir := useRecordingSSH(t)
	program := filepath.Join(t.TempDir(), "shop")
	content := bytes.Repeat([]byte("\x7fELF\r\n\n\r\x1a\x00\xff text\r\n"), 50000)
	if err := os.WriteFile(program, content, 0o755); err != nil {
		t.Fatal(err)
	}
	tgt := deployTarget{host: "vps", app: "shop"}
	if err := tgt.upload(context.Background(), program, "20261122T000000Z", io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	gz, err := os.Open(filepath.Join(dir, "stdin"))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	r, err := gzip.NewReader(gz)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("the server got %d bytes (%v), want the %d of the program", len(got), err, len(content))
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args.json"))
	sum := sha256.Sum256(content)
	if !strings.Contains(string(args), hex.EncodeToString(sum[:])) {
		t.Errorf("the script lacks the sum of the program:\n%s", args)
	}
}
