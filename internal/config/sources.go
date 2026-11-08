package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"
)

// Sources is one read of what the program was started with: its arguments, its environment,
// the .env file and the file named by -c. The settings of tifa and those of the app are all
// taken from the same Sources, see Process.
type Sources struct {
	args   []string
	env    map[string]string
	dotenv map[string]string
	// file holds the settings of the file named by -c, by key, and fileName its name as given.
	file     map[string]entry
	fileName string
	// errs are the problems of reading the .env file and the file of -c.
	errs []error
}

// Err reports what went wrong reading the .env file and the file of -c.
func (s *Sources) Err() error { return errors.Join(s.errs...) }

// Read gathers the sources from args (without the program name), environ (in os.Environ
// form) and the .env file in dir. dir empty reads no .env, and a missing one is no error.
func Read(args, environ []string, dir string) *Sources {
	s := &Sources{args: args, env: map[string]string{}}
	for _, kv := range environ {
		name, value, _ := strings.Cut(kv, "=")
		s.env[name] = value
	}
	if dir != "" {
		data, err := os.ReadFile(filepath.Join(dir, ".env"))
		switch {
		case errors.Is(err, os.ErrNotExist):
		case err != nil:
			s.errs = append(s.errs, fmt.Errorf("config: %w", err))
		default:
			s.dotenv, err = parseDotenv(data)
			if err != nil {
				s.errs = append(s.errs, fmt.Errorf("config: .env %w", err))
			}
		}
	}
	path, err := configFlag(args)
	if err != nil {
		s.errs = append(s.errs, err)
	}
	if path != "" {
		s.fileName = filepath.Base(path)
		data, err := os.ReadFile(path)
		if err != nil {
			s.errs = append(s.errs, fmt.Errorf("config: %w", err))
		} else if s.file, err = yamlEntries(data); err != nil {
			s.errs = append(s.errs, fmt.Errorf("config: file %s: %w", s.fileName, err))
		}
	}
	return s
}

var (
	processOnce    sync.Once
	processSources *Sources
)

// Process returns the sources of this process, read once: os.Args, the environment, and the
// .env file of the working directory.
func Process() *Sources {
	processOnce.Do(func() { processSources = Read(os.Args[1:], os.Environ(), ".") })
	return processSources
}

// cutArgs returns the arguments before a "--".
func cutArgs(args []string) []string {
	if i := slices.Index(args, "--"); i >= 0 {
		return args[:i]
	}
	return args
}

// isValue reports whether the argument after a flag can be its value. A word that starts with -
// is a flag, so a value that starts with - is written after =, such as --ssr-vms=-1.
func isValue(args []string, i int) bool { return i < len(args) && !strings.HasPrefix(args[i], "-") }

// configFlag returns the path given with -c path, -c=path, --config path or --config=path, the
// last one when there are several.
func configFlag(args []string) (string, error) {
	args = cutArgs(args)
	path := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-c" || a == "--config":
			if !isValue(args, i+1) {
				return "", fmt.Errorf("config: flag %s needs a file", a)
			}
			i++
			path = args[i]
		case strings.HasPrefix(a, "--config="):
			path = strings.TrimPrefix(a, "--config=")
		case strings.HasPrefix(a, "-c="):
			path = strings.TrimPrefix(a, "-c=")
		}
	}
	return path, nil
}

// flag returns the value of the flag --name in the arguments, the last one when it is given
// several times. A bool flag takes its value only after =, such as --server-hsts=false, and is
// true without one. Another flag takes the next argument when it has no =, unless that starts
// with -, see isValue.
func (s *Sources) flag(name string, isBool bool) (raw string, ok bool, err error) {
	args := cutArgs(s.args)
	for i := 0; i < len(args); i++ {
		a, found := strings.CutPrefix(args[i], "--")
		if !found {
			continue
		}
		n, v, eq := strings.Cut(a, "=")
		if n != name {
			continue
		}
		switch {
		case eq:
			raw = v
		case isBool:
			raw = "true"
		case isValue(args, i+1):
			i++
			raw = args[i]
		default:
			return "", false, fmt.Errorf("config: flag --%s needs a value", name)
		}
		ok = true
	}
	return raw, ok, nil
}

// apply sets v from each source that names key, the lowest first, so a higher source wins. settings is the project tifa.yaml
// as text, nil for the options of the app. It returns the source of the value, empty for the
// default, and the errors of the values that are not valid.
func (s *Sources) apply(key string, v value, settings map[string]string) (from string, errs []error) {
	// The value of a secret is in no error, since the errors go to the terminal and the log:
	// secretValue.Set takes any text, and entry.check leaves the value out.
	_, secret := v.(secretValue)
	set := func(label, where, raw string) {
		if err := v.Set(raw); err != nil {
			errs = append(errs, fmt.Errorf("config: %s: %s: %w", where, key, err))
			return
		}
		from = label
	}
	if raw, ok := settings[key]; ok {
		set("tifa.yaml", "tifa.yaml", raw)
	}
	if e, ok := s.file[key]; ok {
		label := "file " + s.fileName
		where := fmt.Sprintf("%s line %d", label, e.line)
		if err := e.check(v.kind(), secret); err != nil {
			errs = append(errs, fmt.Errorf("config: %s: %s: %w", where, key, err))
		} else {
			set(label, where, e.raw)
		}
	}
	env := EnvName(key)
	for _, layer := range []struct {
		label string
		vars  map[string]string
	}{{".env", s.dotenv}, {"env", s.env}} {
		if raw, ok := layer.vars[env]; ok {
			set(layer.label+" "+env, layer.label+" "+env, raw)
		} else if key == "server.addr" {
			if label, raw, err := platformAddr(layer.label, layer.vars, v.String()); err != nil {
				errs = append(errs, err)
			} else if label != "" {
				set(label, label, raw)
			}
		}
	}
	name := FlagName(key)
	if raw, ok, err := s.flag(name, v.kind() == "bool"); err != nil {
		errs = append(errs, err)
	} else if ok && secret {
		errs = append(errs, fmt.Errorf("config: flag --%s: %s is a secret, and a flag shows in the process list to every user of the machine: set %s in the environment or in .env, or %s in the file of -c", name, key, EnvName(key), key))
	} else if ok {
		set("flag --"+name, "flag --"+name, raw)
	}
	return from, errs
}

// platformAddr reads the PORT and HOST variables that hosting platforms set, for server.addr
// when SERVER_ADDR is not set. PORT alone listens on every interface, HOST alone keeps the port
// of addr, the address the sources below gave. Empty ones count as not set.
func platformAddr(layer string, vars map[string]string, addr string) (label, raw string, err error) {
	port, host := vars["PORT"], vars["HOST"]
	switch {
	case port == "" && host == "":
		return "", "", nil
	case port != "" && host != "":
		label = layer + " PORT and HOST"
	case port != "":
		label = layer + " PORT"
	default:
		label = layer + " HOST"
	}
	if port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 0 || n > 65535 || strconv.Itoa(n) != port {
			return "", "", fmt.Errorf("config: %s PORT: server.addr: invalid port %q, use a number from 0 to 65535", layer, port)
		}
	} else {
		_, p, err := net.SplitHostPort(addr)
		if err != nil {
			return "", "", fmt.Errorf("config: %s HOST: server.addr: cannot take the port of %q: %v", layer, addr, err)
		}
		port = p
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if strings.ContainsAny(host, "/ ") || strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return "", "", fmt.Errorf("config: %s HOST: server.addr: invalid host %q, write it without a port, such as 0.0.0.0", layer, host)
	}
	return label, net.JoinHostPort(host, port), nil
}

// entry is one setting read from a file. typ is the type the file gave the value: string,
// number, bool or null.
type entry struct {
	raw, typ string
	line     int
	err      error
}

// check reports why the entry cannot set a setting of the given kind, with the value unless the
// setting is a secret. Flags and environment variables are text and skip this check.
func (e entry) check(kind string, secret bool) error {
	if e.err != nil {
		return e.err
	}
	ok := false
	switch kind {
	case "int":
		ok = e.typ == "number"
	case "size":
		ok = e.typ == "string" || e.typ == "number" // 1MiB or a plain byte count
	case "bool":
		ok = e.typ == "bool"
	default:
		ok = e.typ == "string"
	}
	if !ok {
		if secret {
			return fmt.Errorf("expected a %s value, got %s", kind, e.typ)
		}
		return fmt.Errorf("expected a %s value, got %s %q", kind, e.typ, e.raw)
	}
	return nil
}

// decodeYAML reads the next document of dec into n. Every document of a file is read with it, so
// that no error carries text of the file: the errors of the YAML parser hold none, but for the one
// of an alias that names no anchor, which quotes the name, and an unquoted password may start
// with *. io.EOF comes back as it is.
func decodeYAML(dec *yaml.Decoder, n *yaml.Node) error {
	err := dec.Decode(n)
	if err != nil && strings.HasPrefix(err.Error(), "yaml: unknown anchor ") {
		return errors.New("a value starts with *, which YAML reads as an alias of an anchor the file does not have: put the value in quotes")
	}
	return err
}

// yamlEntries reads a YAML file of nested mappings into entries by dotted key: server.addr is
// the key addr in the mapping server.
func yamlEntries(data []byte) (map[string]entry, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := decodeYAML(dec, &doc); err == io.EOF {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var extra yaml.Node
	if err := decodeYAML(dec, &extra); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("line %d: only one YAML document is allowed", extra.Line)
		}
		return nil, err
	}
	if len(doc.Content) == 0 {
		return nil, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("line %d: expected a mapping of sections", root.Line)
	}
	entries := map[string]entry{}
	var walk func(prefix string, m *yaml.Node)
	walk = func(prefix string, m *yaml.Node) {
		for i := 0; i < len(m.Content); i += 2 {
			k, v := m.Content[i], m.Content[i+1]
			key := prefix + k.Value
			switch v.Kind {
			case yaml.MappingNode:
				// A section is no setting, so its entry counts only when its key is one.
				entries[key] = entry{line: k.Line, err: errors.New("expected a single value")}
				walk(key+".", v)
			case yaml.ScalarNode:
				entries[key] = entry{raw: v.Value, typ: yamlType(v.ShortTag()), line: k.Line}
			default:
				entries[key] = entry{line: k.Line, err: errors.New("expected a single value")}
			}
		}
	}
	walk("", root)
	return entries, nil
}

// yamlType names the type of a scalar by its tag. Only the tags yaml.v3 gives a scalar of
// itself are named: any other tag, such as !abc or !!abc, is text the file wrote, and an
// unquoted password may start with one, so it goes into no error.
func yamlType(tag string) string {
	switch tag {
	case "!!str":
		return "string"
	case "!!int", "!!float":
		return "number"
	case "!!bool":
		return "bool"
	case "!!null":
		return "null"
	case "!!timestamp":
		return "timestamp"
	case "!!binary":
		return "binary"
	}
	return "a value with a tag"
}

var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// parseDotenv reads a .env file: lines of NAME=value, blank lines and lines starting with #.
// A line may start with export. A value in double quotes may hold \n, \t, \" and \\, one in
// single quotes is taken as it is, and an unquoted one ends at a # after a space or a tab,
// which may be right after the =, and loses the spaces around it.
func parseDotenv(data []byte) (map[string]string, error) {
	vars := map[string]string{}
	for n, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		where := fmt.Sprintf("line %d", n+1)
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "export "); ok {
			line = strings.TrimSpace(rest)
		}
		name, value, ok := strings.Cut(line, "=")
		name = strings.TrimSpace(name)
		if !ok || !envKey.MatchString(name) {
			return nil, fmt.Errorf("%s: expected NAME=value", where)
		}
		raw := value
		value = strings.TrimSpace(value)
		var err error
		switch {
		case strings.HasPrefix(value, `"`):
			value, err = doubleQuoted(value)
		case strings.HasPrefix(value, "'"):
			end := strings.IndexByte(value[1:], '\'')
			if end < 0 {
				err = errors.New("the single quote is not closed on this line")
			} else {
				err = afterQuote(value[end+2:])
				value = value[1 : end+1]
			}
		default:
			value = strings.TrimSpace(cutComment(raw))
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", where, name, err)
		}
		vars[name] = value
	}
	return vars, nil
}

// cutComment drops a comment from an unquoted value: a # after a space or a tab.
func cutComment(value string) string {
	for i := 1; i < len(value); i++ {
		if value[i] == '#' && (value[i-1] == ' ' || value[i-1] == '\t') {
			return value[:i]
		}
	}
	return value
}

func doubleQuoted(value string) (string, error) {
	var b strings.Builder
	for i := 1; i < len(value); i++ {
		c := value[i]
		switch {
		case c == '"':
			return b.String(), afterQuote(value[i+1:])
		case c == '\\' && i+1 < len(value):
			i++
			switch value[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '"', '\\', '$':
				b.WriteByte(value[i])
			default:
				b.WriteByte('\\')
				b.WriteByte(value[i])
			}
		default:
			b.WriteByte(c)
		}
	}
	return "", errors.New("the double quote is not closed on this line")
}

// afterQuote checks what follows a closing quote: nothing, or a comment.
func afterQuote(rest string) error {
	rest = strings.TrimSpace(rest)
	if rest != "" && !strings.HasPrefix(rest, "#") {
		return errors.New("unexpected text after the closing quote, end the value there or start a comment with #")
	}
	return nil
}

var keyPattern = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)+$`)

// CheckKey reports why key cannot name a setting.
func CheckKey(key string) error {
	if !keyPattern.MatchString(key) {
		return fmt.Errorf("%q is not a setting key: use lower case letters, digits and underscores, in at least two parts joined with dots, such as db.url", key)
	}
	return nil
}

// FlagName is the flag of key without its two hyphens: server.body_limit is server-body-limit.
func FlagName(key string) string { return strings.NewReplacer(".", "-", "_", "-").Replace(key) }

// EnvName is the environment variable of key: server.body_limit is SERVER_BODY_LIMIT.
func EnvName(key string) string { return strings.ToUpper(strings.ReplaceAll(key, ".", "_")) }

// ReservedFlags are flags of the program that are not settings.
var ReservedFlags = []string{"config", "dist", "lifeline-fd", "help"}

// Clash reports why key cannot be an option of the app next to the keys of others: its flag or
// environment variable is that of another key, of tifa or of the app, or a flag of the program,
// or one of the two keys is a section of the other, as db.url is of db.url.timeout.
func Clash(key string, others []string) error {
	flag, env := FlagName(key), EnvName(key)
	if slices.Contains(ReservedFlags, flag) {
		return fmt.Errorf("setting %s has the flag --%s, which the program has already", key, flag)
	}
	for _, other := range append(Keys(), others...) {
		switch {
		case other == key:
			return fmt.Errorf("setting %s is defined twice, or is a setting of tifa", key)
		case strings.HasPrefix(key, other+"."), strings.HasPrefix(other, key+"."):
			// A key is a value in YAML and in ./app config, so it cannot also hold other keys.
			return fmt.Errorf("settings %s and %s make %s a value and a section", key, other, min(key, other))
		case FlagName(other) == flag:
			return fmt.Errorf("settings %s and %s both have the flag --%s", key, other, flag)
		case EnvName(other) == env:
			return fmt.Errorf("settings %s and %s both have the environment variable %s", key, other, env)
		}
	}
	return nil
}

// Args returns the arguments the sources were read from.
func (s *Sources) Args() []string { return s.args }
