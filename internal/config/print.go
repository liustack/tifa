package config

import (
	"fmt"
	"strconv"
	"strings"
)

// Print writes the runtime settings of tifa in c and the options of the app as a commented
// YAML file, which a deployment can pass with -c. from gives the source of each value of
// tifa as Load returns it. Each comment names the flag, the environment variable and the
// source of the value.
func Print(c Config, from map[string]string, appSettings []AppSetting) []byte {
	type line struct {
		key, help, kind, value, from string
		secret                       bool
	}
	var lines []line
	for _, f := range runtimeFields(&c) {
		lines = append(lines, line{key: f.key, help: f.help, kind: f.value.kind(), value: f.value.String(), from: from[f.key]})
	}
	for _, s := range appSettings {
		lines = append(lines, line{s.Key, s.Help, s.Kind, s.Value, s.From, s.Secret})
	}
	// The keys go into a tree that keeps the order in which each part first appears, so keys
	// that share a part are written under one mapping.
	type node struct {
		name  string
		kids  []*node
		value *line
	}
	root := &node{}
	for i := range lines {
		n := root
		for _, part := range strings.Split(lines[i].key, ".") {
			var next *node
			for _, k := range n.kids {
				if k.name == part {
					next = k
				}
			}
			if next == nil {
				next = &node{name: part}
				n.kids = append(n.kids, next)
			}
			n = next
		}
		n.value = &lines[i]
	}
	var b strings.Builder
	b.WriteString("# The settings of the program, each with where its value comes from. Precedence from high\n")
	b.WriteString("# to low: flags, environment variables, .env, the file of -c, tifa.yaml, defaults.\n")
	var write func(n *node, depth int)
	write = func(n *node, depth int) {
		indent := strings.Repeat("  ", depth)
		if l := n.value; l != nil {
			source := l.from
			if source == "" {
				source = "default"
			}
			help := strings.TrimSuffix(l.help, ".")
			if help != "" {
				help += ". "
			}
			fmt.Fprintf(&b, "%s# %sFlag --%s, env %s. From %s.\n", indent, help, FlagName(l.key), EnvName(l.key), source)
			// A secret that a source set is left out as a comment: the output is pasted into
			// issues and chats, and read back with -c, where a placeholder would become the value.
			if l.secret && l.from != "" {
				fmt.Fprintf(&b, "%s# %s: (hidden)\n", indent, n.name)
				return
			}
			value := l.value
			if l.kind == "string" || l.kind == "strings" {
				value = strconv.Quote(value)
			}
			fmt.Fprintf(&b, "%s%s: %s\n", indent, n.name, value)
			return
		}
		if depth >= 0 && n != root {
			fmt.Fprintf(&b, "%s%s:\n", indent, n.name)
		}
		for _, k := range n.kids {
			write(k, depth+1)
		}
	}
	for _, k := range root.kids {
		write(k, 0)
	}
	return []byte(b.String())
}
