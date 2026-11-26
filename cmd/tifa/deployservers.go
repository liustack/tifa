package main

// The servers this person has set up or deployed to, kept in ~/.tifa/config.yaml (TIFA_HOME moves
// the folder, and on Windows the folder is %USERPROFILE%\.tifa). tifa deploy setup records a server
// after it set it up, and tifa deploy after a release went live, so a server set up from another
// machine gets its entry too. tifa deploy remove takes the app off its entry, and a deploy of an
// app that names no server uses the one in the file. The file holds no secret: for each server an
// ID, the host that last worked and the apps set up or deployed there from this machine.
//
// A server is known by what the probes read from it, the host name and the start of the machine ID
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
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// tifaHomeEnv moves the folder that holds config.yaml.
const tifaHomeEnv = "TIFA_HOME"

const serversFileName = "config.yaml"

const serversHeader = "# Servers this machine has set up or deployed to with tifa deploy. tifa deploy uses the only one when an app names none.\n" +
	"# No secrets are kept here. To forget a server, delete its entry."

// knownApp is an app set up or deployed on a server from this machine. It is known by the folder of its
// checkout, so two checkouts that share a name are two apps. The name is for messages.
type knownApp struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
	// Host is what ssh took as the destination for this app, the one that last worked. With ssh
	// through the tunnel it is the ssh host name of the app's domain, which differs per app.
	Host string `yaml:"host"`
}

// knownServer is one entry of the file.
type knownServer struct {
	// ID is the host name of the server and the start of its machine ID, as the probes read them.
	ID string `yaml:"id"`
	// Host is the one a new app uses: the host that worked last.
	Host string `yaml:"host"`
	// Apps are the apps set up or deployed on the server from this machine.
	Apps []knownApp `yaml:"apps"`
}

// name is the host name of the server, the ID without its machine ID part.
func (s knownServer) name() string {
	if i := strings.LastIndex(s.ID, "-"); i > 0 {
		return s.ID[:i]
	}
	return s.ID
}

// appNames lists the names of the apps, for messages.
func (s knownServer) appNames() string {
	if len(s.Apps) == 0 {
		return "none"
	}
	names := make([]string, len(s.Apps))
	for i, a := range s.Apps {
		names[i] = a.Name
	}
	return strings.Join(names, ", ")
}

// samePath says two folders of this machine are the same. Windows paths ignore case.
func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func (s knownServer) appAt(path string) (knownApp, bool) {
	for _, a := range s.Apps {
		if samePath(a.Path, path) {
			return a, true
		}
	}
	return knownApp{}, false
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

// pickServer chooses the server of the app whose checkout is at path, and the host to reach it
// by: the server that lists the folder, with the host that worked for that app, else the only
// server there is, with its host. It reports false when there is none, or no way to tell.
func pickServer(servers []knownServer, path string) (s knownServer, host string, ok bool) {
	var has []knownServer
	for _, c := range servers {
		if _, found := c.appAt(path); found {
			has = append(has, c)
		}
	}
	switch {
	case len(has) == 1:
		a, _ := has[0].appAt(path)
		return has[0], a.Host, true
	case len(has) == 0 && len(servers) == 1:
		return servers[0], servers[0].Host, true
	}
	return knownServer{}, "", false
}

// rememberServer records that app is set up on the server id, reached by host. The server's host
// becomes this one, for the next new app. The app keeps its own host. Any other entry with the
// same host goes, since that place holds one server, and the app leaves any other server it was
// on. It reports whether the file changed.
func rememberServer(id, host string, app knownApp) (changed bool, err error) {
	app.Host = host
	return changeServers(true, func(list []knownServer) []knownServer {
		var out []knownServer
		found := false
		for _, s := range list {
			if s.ID != id && s.Host == host {
				continue
			}
			s.Apps = slices.DeleteFunc(slices.Clone(s.Apps), func(a knownApp) bool { return s.ID != id && samePath(a.Path, app.Path) })
			if s.ID != id {
				if len(s.Apps) == 0 {
					continue
				}
				out = append(out, s)
				continue
			}
			found = true
			s.Host = host
			if i := slices.IndexFunc(s.Apps, func(a knownApp) bool { return samePath(a.Path, app.Path) }); i >= 0 {
				s.Apps[i] = app
			} else {
				s.Apps = append(s.Apps, app)
			}
			out = append(out, s)
		}
		if !found {
			out = append(out, knownServer{ID: id, Host: host, Apps: []knownApp{app}})
		}
		return out
	})
}

// rememberAndSay is rememberServer for a command that reached the server: it says so in one line
// when the file changed. A file it cannot write is a warning, since what the command did on the
// server stands.
func rememberAndSay(id, host string, app knownApp, stdout, stderr io.Writer) {
	changed, err := rememberServer(id, host, app)
	switch {
	case err != nil:
		fmt.Fprintf(stderr, "Could not remember the server in %s: %v\n", shownServersPath(), err)
	case changed:
		s := knownServer{ID: id}
		line := fmt.Sprintf("Remembered %s (%s) in %s", s.name(), host, shownServersPath())
		if all, err := readServers(); err == nil && len(all) == 1 {
			line += fmt.Sprintf(", so the next app on this machine needs no %s", deployHostEnv)
		}
		fmt.Fprintln(stdout, line+".")
	}
}

// forgetServerApp drops the app at path from the server id, and the server with its last app. When
// the server's host was the one of that app, it becomes the host of an app that stays, so the next
// new app is not sent to a host that is gone. A server or a file that is not there is not an error.
func forgetServerApp(id, path string) error {
	_, err := changeServers(false, func(list []knownServer) []knownServer {
		var out []knownServer
		for _, s := range list {
			if s.ID == id {
				gone, ok := s.appAt(path)
				s.Apps = slices.DeleteFunc(slices.Clone(s.Apps), func(a knownApp) bool { return samePath(a.Path, path) })
				if len(s.Apps) == 0 {
					continue
				}
				if ok && s.Host == gone.Host && !slices.ContainsFunc(s.Apps, func(a knownApp) bool { return a.Host == s.Host }) {
					s.Host = s.Apps[len(s.Apps)-1].Host
				}
			}
			out = append(out, s)
		}
		return out
	})
	return err
}

// changeServers runs edit on the file under the lock and writes the result. With create false a
// missing file stays missing. It reports whether the file was written.
func changeServers(create bool, edit func([]knownServer) []knownServer) (bool, error) {
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
	list, err := tree.servers(path)
	if err != nil {
		return false, err
	}
	tree.apply(edit(list))
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
	seen := map[string]bool{}
	for i, s := range list {
		switch {
		case s.ID == "" || s.Host == "":
			return nil, fmt.Errorf("%s: server %d needs an id and a host", path, i+1)
		case seen[s.ID]:
			return nil, fmt.Errorf("%s: the id %s is listed twice: delete one entry", path, s.ID)
		}
		seen[s.ID] = true
		for _, a := range s.Apps {
			if a.Name == "" || a.Path == "" || a.Host == "" {
				return nil, fmt.Errorf("%s: an app of %s needs a name, a path and a host", path, s.ID)
			}
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

func str(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func setString(m *yaml.Node, key, value string) {
	v := field(m, key)
	*v = yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value, LineComment: v.LineComment}
}

// apply makes the entries of the tree the list: an entry that was there keeps its place and what
// else a person wrote in it, a new one is added, one that is not in the list goes.
func (t *serversTree) apply(list []knownServer) {
	old := map[string]*yaml.Node{}
	for _, item := range t.seq.Content {
		var s knownServer
		if item.Decode(&s) == nil {
			old[s.ID] = item
		}
	}
	var content []*yaml.Node
	for _, s := range list {
		item := old[s.ID]
		if item == nil {
			item = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setString(item, "id", s.ID)
		}
		setString(item, "host", s.Host)
		apps := field(item, "apps")
		*apps = yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", LineComment: apps.LineComment}
		for _, a := range s.Apps {
			apps.Content = append(apps.Content, &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Style: yaml.FlowStyle,
				Content: []*yaml.Node{str("name"), str(a.Name), str("path"), str(a.Path), str("host"), str(a.Host)}})
		}
		content = append(content, item)
	}
	t.seq.Content = content
	t.seq.Style = 0
	if len(content) == 0 {
		t.seq.Style = yaml.FlowStyle
	}
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
