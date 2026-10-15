package ssr

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/liustack/quickjs"
)

// scriptError is an exception of the bundle's JavaScript that the bundle caught and reported in
// renderResult as "name: message", a newline and its stack trace.
type scriptError struct {
	text string
}

func (e *scriptError) Error() string {
	head, _, _ := strings.Cut(e.text, "\n")
	return "ssr: render page: " + head
}

// explainedError is an error of the bundle's JavaScript with its stack trace, the places in it
// given in the sources, and for a missing require what to do.
type explainedError struct {
	err  error
	text string
}

func (e *explainedError) Error() string { return e.text }
func (e *explainedError) Unwrap() error { return e.err }

// bundlePlace matches a place in the bundle in a QuickJS stack trace. QuickJS names the bundle
// <eval>, and code that eval or Function runs <input>.
var bundlePlace = regexp.MustCompile(`<eval>:(\d+):(\d+)`)

// nodeModule matches the call that loads a Node module packages import, and its packages, see
// the tifa Vite plugin.
var nodeModule = regexp.MustCompile("^require\\([\"`]([^\"`]*)[\"`](?:, ?[\"`]([^\"`]*)[\"`])?\\)")

// maxFrames is how many frames of a stack trace an error gives, the rest it counts. An error is
// explained after its render, outside the limits of the VM, so its cost must have a limit too.
const maxFrames = 64

// markEvery is how many code points apart the marks of a long line are.
const markEvery = 1024

// missingRequire is how QuickJS reports a call of require, which it does not have.
const missingRequire = "ReferenceError: 'require' is not defined"

// sources turns places in the bundle into places in the files it came from, by the source map
// of the bundle. The first time it is used, on the first error, it indexes the lines of the
// bundle and decodes the map. Each place then costs at most markEvery code points of a line.
type sources struct {
	code string // the bundle
	raw  []byte // its source map, nil without one

	once   sync.Once
	starts []int          // where each line of the bundle starts in code
	marks  map[int][]mark // by line, for lines longer than markEvery code points
	files  []string
	lines  [][]segment // by line of the bundle, in the order of the column
}

// mark is where code point k*markEvery of a long line is, for the kth mark of the line: its
// offset in the bundle and its column in UTF-16 units.
type mark struct {
	offset, units int
}

// segment says that the bundle from column on comes from line and column of files[file], all
// from 0, or from no file when file is -1.
type segment struct {
	column, file, line, sourceColumn int
}

// explain returns err with the stack trace of the exception of the bundle it carries, the places
// in it given in the sources, and for a missing require which code called it and what to do.
// Errors that carry no stack trace come back as they are.
func (s *sources) explain(err error) error {
	var head, stack string
	var reported *scriptError
	var js *quickjs.Error
	switch {
	case errors.As(err, &reported):
		head, stack, _ = strings.Cut(reported.text, "\n")
	case errors.As(err, &js):
		head, stack = js.Name+": "+js.Message, js.Stack
	}
	if strings.TrimSpace(stack) == "" {
		return err
	}
	frames := strings.Split(strings.TrimRight(stack, "\n"), "\n")
	more := len(frames) - maxFrames
	if more > 0 {
		frames = frames[:maxFrames]
	}
	text := err.Error() + "\n" + bundlePlace.ReplaceAllStringFunc(strings.Join(frames, "\n"), func(place string) string {
		line, column := placeOf(place)
		return s.name(line, column)
	})
	if more > 0 {
		text += fmt.Sprintf("\n    ... %d more frames", more)
	}
	if head == missingRequire {
		if place := bundlePlace.FindString(stack); place != "" {
			text += "\n" + s.requireAdvice(placeOf(place))
		}
	}
	return &explainedError{err: err, text: text}
}

// placeOf reads the line and column, both from 1, of a match of bundlePlace.
func placeOf(place string) (int, int) {
	m := bundlePlace.FindStringSubmatch(place)
	line, _ := strconv.Atoi(m[1])
	column, _ := strconv.Atoi(m[2])
	return line, column
}

// requireAdvice says which code called require at line and column of the bundle, why it fails,
// and what to do.
func (s *sources) requireAdvice(line, column int) string {
	const why = ". SSR runs in QuickJS, which has no require, so this code took a path meant for Node. " +
		"Use the browser entry of the package, or call it from a .client file or a useEffect, which run in the browser only."
	file, sourceLine, sourceColumn, ok := s.source(line, column)
	if !ok {
		// The bundle's function gets the Node modules packages import as its arguments.
		call := nodeModule.FindStringSubmatch(s.text(line, column))
		if call != nil && call[2] != "" {
			return fmt.Sprintf("The bundle loads the Node module %q, imported by %s, at %s", call[1], call[2], s.name(line, column)) + why
		}
		if call != nil {
			return fmt.Sprintf("The bundle loads the Node module %q at %s", call[1], s.name(line, column)) + why
		}
		return fmt.Sprintf("The code at %s calls require", s.name(line, column)) + why
	}
	place := fmt.Sprintf("%s:%d:%d", file, sourceLine+1, sourceColumn+1)
	if name := packageName(file); name != "" {
		return fmt.Sprintf("The package %s calls require at %s", name, place) + why
	}
	return fmt.Sprintf("The code at %s calls require", place) + why
}

// name names line and column of the bundle, both from 1, by its place in the sources, or in
// ssr.js when no source maps to it.
func (s *sources) name(line, column int) string {
	if file, sourceLine, sourceColumn, ok := s.source(line, column); ok {
		return fmt.Sprintf("%s:%d:%d", file, sourceLine+1, sourceColumn+1)
	}
	return fmt.Sprintf("ssr.js:%d:%d", line, column)
}

// source returns the file, line and column, both from 0, that line and column of the bundle,
// both from 1, come from.
func (s *sources) source(line, column int) (string, int, int, bool) {
	_, units, ok := s.locate(line, column)
	if !ok || line > len(s.lines) {
		return "", 0, 0, false
	}
	segments := s.lines[line-1]
	i := sort.Search(len(segments), func(i int) bool { return segments[i].column > units }) - 1
	if i < 0 || segments[i].file < 0 {
		return "", 0, 0, false
	}
	return s.files[segments[i].file], segments[i].line, segments[i].sourceColumn, true
}

// locate returns the offset in the bundle of line and column, both from 1, and the column in
// UTF-16 units from 0. QuickJS counts columns in code points, source maps in UTF-16 units.
func (s *sources) locate(line, column int) (offset, units int, ok bool) {
	s.once.Do(s.prepare)
	if line < 1 || line > len(s.starts) || column < 1 {
		return 0, 0, false
	}
	end := len(s.code)
	if line < len(s.starts) {
		end = s.starts[line] - 1
	}
	offset, from := s.starts[line-1], 0
	if marks := s.marks[line-1]; marks != nil {
		k := min((column-1)/markEvery, len(marks)-1)
		offset, units, from = marks[k].offset, marks[k].units, k*markEvery
	}
	for range column - 1 - from {
		if offset >= end {
			return 0, 0, false
		}
		r, size := utf8.DecodeRuneInString(s.code[offset:end])
		offset += size
		units += utf16.RuneLen(r)
	}
	return offset, units, true
}

// text returns the start of line of the bundle from column on, both from 1.
func (s *sources) text(line, column int) string {
	offset, _, ok := s.locate(line, column)
	if !ok {
		return ""
	}
	rest := s.code[offset:]
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[:i]
	}
	return rest[:min(len(rest), 512)]
}

// prepare indexes the lines of the bundle and decodes the source map.
func (s *sources) prepare() {
	s.starts = []int{0}
	s.marks = map[int][]mark{}
	var marks []mark
	points, units := 0, 0
	for i, r := range s.code {
		if r == '\n' {
			if marks != nil {
				s.marks[len(s.starts)-1] = marks
				marks = nil
			}
			s.starts = append(s.starts, i+1)
			points, units = 0, 0
			continue
		}
		if points > 0 && points%markEvery == 0 {
			if marks == nil {
				marks = []mark{{s.starts[len(s.starts)-1], 0}}
			}
			marks = append(marks, mark{i, units})
		}
		points++
		units += utf16.RuneLen(r)
	}
	if marks != nil {
		s.marks[len(s.starts)-1] = marks
	}
	s.decode()
}

// decode reads the source map. A map it cannot read maps nothing, the places stay in ssr.js.
func (s *sources) decode() {
	if s.raw == nil {
		return
	}
	var m struct {
		Sources  []string `json:"sources"`
		Mappings string   `json:"mappings"`
	}
	if json.Unmarshal(s.raw, &m) != nil {
		return
	}
	s.files = m.Sources
	file, line, column := 0, 0, 0
	for text := range strings.SplitSeq(m.Mappings, ";") {
		var segments []segment
		generated := 0
		for field := range strings.SplitSeq(text, ",") {
			values, ok := vlq(field)
			if !ok || len(values) == 0 {
				continue
			}
			generated += values[0]
			if len(values) < 4 {
				segments = append(segments, segment{column: generated, file: -1})
				continue
			}
			file, line, column = file+values[1], line+values[2], column+values[3]
			if file < 0 || file >= len(s.files) {
				segments = append(segments, segment{column: generated, file: -1})
				continue
			}
			segments = append(segments, segment{generated, file, line, column})
		}
		s.lines = append(s.lines, segments)
	}
}

// vlq decodes the Base64 VLQ numbers of one source map segment.
func vlq(field string) ([]int, bool) {
	const digits = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var values []int
	value, shift := 0, 0
	for i := range len(field) {
		digit := strings.IndexByte(digits, field[i])
		if digit < 0 || shift > 30 {
			return nil, false
		}
		value |= (digit & 31) << shift
		if digit&32 != 0 {
			shift += 5
			continue
		}
		if value&1 != 0 {
			values = append(values, -(value >> 1))
		} else {
			values = append(values, value>>1)
		}
		value, shift = 0, 0
	}
	return values, shift == 0
}

// packageName returns the npm package that file belongs to, by the last node_modules in its
// path, or "" for a file outside node_modules.
func packageName(file string) string {
	parts := strings.Split(file, "/")
	for i := len(parts) - 2; i >= 0; i-- {
		if parts[i] != "node_modules" {
			continue
		}
		if strings.HasPrefix(parts[i+1], "@") && i+2 < len(parts) {
			return parts[i+1] + "/" + parts[i+2]
		}
		return parts[i+1]
	}
	return ""
}
