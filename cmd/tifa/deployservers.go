package main

// The servers this person has set up, kept in ~/.tifa/config.yaml (TIFA_HOME moves the folder, and
// on Windows the folder is %USERPROFILE%\.tifa). tifa deploy setup records a server after it set
// it up, tifa deploy remove takes the app off its entry, and a deploy of an app that names no
// server uses the one in the file. The file holds no secret: for each server an ID, the host
// that last worked and the apps setup put there from this machine.
//
// A server is known by what setup reads from it, the host name and the start of the machine ID
// (serverState.Machine), and not by the host it is reached by, since that changes: an address
// first, the ssh host name of the tunnel later.
//
// Writing goes through a lock file next to the config, so two tifa processes do not lose each
// other's entry, and through a temporary file and a rename, so a reader never sees half a file.
// The file is edited as a YAML tree, which keeps the comments and keys a person added.

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// tifaHomeEnv moves the folder that holds config.yaml.
const tifaHomeEnv = "TIFA_HOME"

const serversFileName = "config.yaml"

const serversHeader = "# Servers tifa deploy setup has set up from this machine. tifa deploy uses the only one when an app names none.\n" +
	"# No secrets are kept here. To forget a server, delete its entry."

// knownServer is one entry of the file.
type knownServer struct {
	// ID is the host name of the server and the start of its machine ID, as setup reads them.
	ID string `yaml:"id"`
	// Host is what ssh takes as the destination, the one that last worked.
	Host string `yaml:"host"`
	// Apps are the apps setup put on the server from this machine.
	Apps []string `yaml:"apps"`
}

// name is the host name of the server, the ID without its machine ID part.
func (s knownServer) name() string {
	if i := strings.LastIndex(s.ID, "-"); i > 0 {
		return s.ID[:i]
	}
	return s.ID
}

// tifaHome is the folder of the config: TIFA_HOME, else .tifa in the home folder of the user.
func tifaHome() (string, error) {
	if dir := os.Getenv(tifaHomeEnv); dir != "" {
		return filepath.Clean(dir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("no home folder for ~/.tifa: %w (set %s to a folder)", err, tifaHomeEnv)
	}
	return filepath.Join(home, ".tifa"), nil
}

// serversPath is the file of the servers.
func serversPath() (string, error) {
	dir, err := tifaHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, serversFileName), nil
}

// shownServersPath is the path for a sentence: ~/.tifa/config.yaml where that is the file.
func shownServersPath() string {
	path, err := serversPath()
	if err != nil {
		return "~/.tifa/" + serversFileName
	}
	if home, err := os.UserHomeDir(); err == nil && os.Getenv(tifaHomeEnv) == "" {
		if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return path
}

// readServers reads the file. A missing file holds no server.
func readServers() ([]knownServer, error) {
	path, err := serversPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	tree, err := parseServers(path, data)
	if err != nil {
		return nil, err
	}
	return tree.servers(path)
}

// pickServer chooses the server of an app that names none: the one that lists the app, else the
// only one there is. It reports false when there is none, or no way to tell.
func pickServer(servers []knownServer, app string) (knownServer, bool) {
	var has []knownServer
	for _, s := range servers {
		if slices.Contains(s.Apps, app) {
			has = append(has, s)
		}
	}
	switch {
	case len(has) == 1:
		return has[0], true
	case len(has) == 0 && len(servers) == 1:
		return servers[0], true
	}
	return knownServer{}, false
}

// rememberServer records that app is set up on the server id, reached by host. It adds the entry
// or updates it, and drops any other entry with the same host: that place holds one server. It
// reports whether the file changed.
func rememberServer(id, host, app string) (changed bool, err error) {
	return changeServers(true, func(t *serversTree, path string) error {
		list, err := t.servers(path)
		if err != nil {
			return err
		}
		for i := len(list) - 1; i >= 0; i-- {
			if list[i].ID != id && list[i].Host == host {
				t.drop(i)
			}
		}
		t.upsert(id, host, app)
		return nil
	})
}

// forgetServerApp drops app from the server id, and the server with its last app. A server or a
// file that is not there is not an error.
func forgetServerApp(id, app string) error {
	_, err := changeServers(false, func(t *serversTree, path string) error {
		list, err := t.servers(path)
		if err != nil {
			return err
		}
		for i, s := range list {
			if s.ID != id {
				continue
			}
			apps := slices.DeleteFunc(slices.Clone(s.Apps), func(a string) bool { return a == app })
			if len(apps) == 0 {
				t.drop(i)
			} else {
				t.setApps(i, apps)
			}
		}
		return nil
	})
	return err
}

// changeServers runs edit on the file under the lock and writes the result. With create false a
// missing file stays missing. It reports whether the file was written.
func changeServers(create bool, edit func(t *serversTree, path string) error) (bool, error) {
	path, err := serversPath()
	if err != nil {
		return false, err
	}
	if !create {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	unlock, err := lockFile(path + ".lock")
	if err != nil {
		return false, err
	}
	defer unlock()
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	tree, err := parseServers(path, data)
	if err != nil {
		return false, err
	}
	before, err := tree.encode()
	if err != nil {
		return false, err
	}
	if err := edit(tree, path); err != nil {
		return false, err
	}
	after, err := tree.encode()
	if err != nil {
		return false, err
	}
	if data != nil && bytes.Equal(before, after) {
		return false, nil
	}
	return true, writeFileAtomic(path, after)
}

// serversTree is the YAML of the file as a tree, so comments and other keys survive an edit.
type serversTree struct {
	doc *yaml.Node
	seq *yaml.Node // the value of servers
}

func parseServers(path string, data []byte) (*serversTree, error) {
	bad := func(err error) error { return fmt.Errorf("%s: %w", path, err) }
	doc := &yaml.Node{}
	if len(bytes.TrimSpace(data)) > 0 {
		if err := yaml.Unmarshal(data, doc); err != nil {
			return nil, bad(err)
		}
	}
	if doc.Kind == 0 {
		doc = &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	top := doc.Content[0]
	if top.Kind != yaml.MappingNode {
		return nil, bad(errors.New("the file is not a map with the key servers"))
	}
	t := &serversTree{doc: doc}
	for i := 0; i+1 < len(top.Content); i += 2 {
		if top.Content[i].Value == "servers" {
			t.seq = top.Content[i+1]
		}
	}
	if t.seq == nil {
		key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "servers", HeadComment: serversHeader}
		t.seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		top.Content = append(top.Content, key, t.seq)
	}
	if t.seq.Kind == yaml.ScalarNode && t.seq.Tag == "!!null" {
		*t.seq = yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	}
	if t.seq.Kind != yaml.SequenceNode {
		return nil, bad(errors.New("servers is not a list"))
	}
	return t, nil
}

// servers reads the entries, and says which one is wrong.
func (t *serversTree) servers(path string) ([]knownServer, error) {
	var list []knownServer
	if err := t.seq.Decode(&list); err != nil {
		return nil, fmt.Errorf("%s: servers: %w", path, err)
	}
	for i, s := range list {
		if s.ID == "" || s.Host == "" {
			return nil, fmt.Errorf("%s: server %d needs an id and a host", path, i+1)
		}
	}
	return list, nil
}

func (t *serversTree) encode() ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(t.doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (t *serversTree) drop(i int) {
	t.seq.Content = slices.Delete(t.seq.Content, i, i+1)
	if len(t.seq.Content) == 0 {
		t.seq.Style = yaml.FlowStyle
	}
}

// field returns the value node of key in an entry, adding it when missing.
func field(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	v := &yaml.Node{}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, v)
	return v
}

func setString(m *yaml.Node, key, value string) {
	v := field(m, key)
	*v = yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value, LineComment: v.LineComment}
}

func (t *serversTree) setApps(i int, apps []string) {
	v := field(t.seq.Content[i], "apps")
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle, LineComment: v.LineComment}
	for _, a := range apps {
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: a})
	}
	*v = *seq
}

// upsert sets the host of the entry id and adds app to it, making the entry when it is new.
func (t *serversTree) upsert(id, host, app string) {
	t.seq.Style = 0
	for i, item := range t.seq.Content {
		var s knownServer
		if err := item.Decode(&s); err != nil || s.ID != id {
			continue
		}
		setString(item, "host", host)
		if !slices.Contains(s.Apps, app) {
			t.setApps(i, append(s.Apps, app))
		}
		return
	}
	item := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	t.seq.Content = append(t.seq.Content, item)
	setString(item, "id", id)
	setString(item, "host", host)
	t.setApps(len(t.seq.Content)-1, []string{app})
}

// writeFileAtomic writes data to path through a temporary file in the same folder and a rename.
// The mode is 0600, which Windows does not have: there it does nothing.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), serversFileName+".tmp*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// Windows refuses a rename over a file that another process has open for a moment.
	for i := 0; ; i++ {
		err = os.Rename(tmp.Name(), path)
		if err == nil || i == 10 {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// serversLockWait is how long a write waits for another tifa to finish its own.
const serversLockWait = 10 * time.Second

// lockFile takes the exclusive lock of the file at path, making the file when it is not there. It
// is the lock of tifa dev and build (flock, LockFileEx): the system drops it when its holder
// ends, so a process that died leaves none behind. The empty file stays.
func lockFile(path string) (unlock func(), err error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	ok, err := tryLockFor(f, serversLockWait)
	if err != nil {
		f.Close()
		return nil, err
	}
	if !ok {
		f.Close()
		return nil, fmt.Errorf("%s is held by another tifa for more than %s", path, serversLockWait)
	}
	return func() { f.Close() }, nil
}
