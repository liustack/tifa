// Package foreign recognizes code written for another framework in a Tifa app: the imports,
// directives, exports and file names of Next.js, Remix and React Router, TanStack Start and
// SvelteKit, and the handlers of other Go web frameworks. Agents that have never seen Tifa write
// these from memory. Each finding says what the code is and what to write in Tifa instead.
//
// One table (rules.go) holds every idiom and its message. A name that Tifa itself allows is never
// reported: where an idiom could also be written on purpose, the rule looks at what is written
// (the kind of value an export holds) before it speaks.
package foreign

import (
	"fmt"
	"sort"
)

// Rule is one idiom of another framework.
type Rule struct {
	// Says names the idiom and the framework it comes from.
	Says string
	// Instead is the Tifa way, as a sentence that starts with what to write.
	Instead string
	// Doc is where to read more, a path in the docs of the Tifa version the app uses (see `go tool
	// tifa docs`), with the anchor of the section.
	Doc string
	// Warning is set for code that builds and does nothing useful, such as an export Tifa never
	// reads. A rule without it reports what cannot be right: an import that cannot resolve.
	Warning bool
}

// Message is the text of a finding: what it is, what to write in Tifa, where to read.
func (r Rule) Message() string {
	return fmt.Sprintf("%s. In Tifa: %s. See %s", r.Says, r.Instead, r.Doc)
}

// Finding is one idiom found in a file.
type Finding struct {
	File    string // relative to the application root, with forward slashes
	Line    int
	Column  int
	Warning bool
	Msg     string
}

// String is the line tifa prints: a warning ends with "(warning)".
func (f Finding) String() string {
	s := fmt.Sprintf("%s:%d: %s", f.File, f.Line, f.Msg)
	if f.Warning {
		s += " (warning)"
	}
	return s
}

func finding(file string, line, column int, r Rule) Finding {
	return Finding{File: file, Line: line, Column: column, Warning: r.Warning, Msg: r.Message()}
}

func sortFindings(fs []Finding) []Finding {
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Column < b.Column
	})
	return fs
}

// Errors returns the findings that are not warnings.
func Errors(fs []Finding) []Finding {
	var out []Finding
	for _, f := range fs {
		if !f.Warning {
			out = append(out, f)
		}
	}
	return out
}

// Warnings returns the findings that are warnings.
func Warnings(fs []Finding) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Warning {
			out = append(out, f)
		}
	}
	return out
}
