// Package sessiontest is the root of the session test app: a store that keeps sessions in a file,
// so that a second process finds the sessions of the first.
package sessiontest

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"sync"
	"time"

	"github.com/liustack/tifa"
)

type fileStore struct {
	mu   sync.Mutex
	path string
}

type fileRecord struct {
	Data    []byte    `json:"data"`
	Expires time.Time `json:"expires"`
}

// load reads the records. A file that does not exist yet holds none.
func (s *fileStore) load() (map[string]fileRecord, error) {
	records := map[string]fileRecord{}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return records, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *fileStore) save(records map[string]fileRecord) error {
	data, err := json.Marshal(records)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

func (s *fileStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.load()
	if err != nil {
		return nil, false, err
	}
	r, ok := records[key]
	if !ok || !time.Now().Before(r.Expires) {
		return nil, false, nil
	}
	return r.Data, true, nil
}

func (s *fileStore) Put(ctx context.Context, key string, data []byte, expiry time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.load()
	if err != nil {
		return err
	}
	records[key] = fileRecord{Data: append([]byte(nil), data...), Expires: expiry}
	return s.save(records)
}

func (s *fileStore) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.load()
	if err != nil {
		return err
	}
	delete(records, key)
	return s.save(records)
}

// OnStart keeps the sessions in the file named by SESSION_FILE when it is set.
func OnStart(ctx context.Context) error {
	if path := os.Getenv("SESSION_FILE"); path != "" {
		tifa.SetSessionStore(&fileStore{path: path})
	}
	return nil
}
