package bundle

import (
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// parsed keeps what a parse of the driver gave for each file, with the hash of the content it
// parsed, so that tifa dev parses again only the files that changed since. The zero value is
// ready.
type parsed[T any] struct {
	mu sync.Mutex
	m  map[string]parsedFile[T] // by absolute path
}

type parsedFile[T any] struct {
	sum [sha256.Size]byte
	v   T
	ok  bool // whether the parse gave a value for the file
}

// read returns what parse returns for files, a map by file that holds a value for the files parse
// gives one. It passes parse only the files whose content it has not kept a result for, in their
// order. As parse stops at the first file that fails, and a file that failed is not kept, the error
// is the one of a parse of all files. A file that cannot be read goes to parse too, which says why.
// The results of other files that are gone, renamed or deleted, are dropped.
func (p *parsed[T]) read(files []string, parse func([]string) (map[string]T, error)) (map[string]T, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]T, len(files))
	type miss struct {
		file, abs string
		sum       [sha256.Size]byte
		hashed    bool
	}
	var todo []miss
	asked := make(map[string]bool, len(files))
	for _, f := range files {
		abs, err := filepath.Abs(f)
		if err != nil {
			return nil, err
		}
		asked[abs] = true
		data, err := os.ReadFile(f)
		if err != nil {
			todo = append(todo, miss{file: f, abs: abs})
			continue
		}
		sum := sha256.Sum256(data)
		if c, ok := p.m[abs]; ok && c.sum == sum {
			if c.ok {
				out[f] = c.v
			}
			continue
		}
		todo = append(todo, miss{file: f, abs: abs, sum: sum, hashed: true})
	}
	for abs := range p.m {
		if asked[abs] {
			continue
		}
		if _, err := os.Stat(abs); errors.Is(err, fs.ErrNotExist) {
			delete(p.m, abs)
		}
	}
	if len(todo) == 0 {
		return out, nil
	}
	names := make([]string, len(todo))
	for i, m := range todo {
		names[i] = m.file
	}
	got, err := parse(names)
	if err != nil {
		return nil, err
	}
	if p.m == nil {
		p.m = map[string]parsedFile[T]{}
	}
	for _, m := range todo {
		v, ok := got[m.file]
		if ok {
			out[m.file] = v
		}
		// Kept only when the file still holds what was hashed, which is then what parse read.
		if !m.hashed {
			continue
		}
		if data, err := os.ReadFile(m.file); err == nil && sha256.Sum256(data) == m.sum {
			p.m[m.abs] = parsedFile[T]{sum: m.sum, v: v, ok: ok}
		}
	}
	return out, nil
}
