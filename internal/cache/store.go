package cache

import (
	"os"
	"path/filepath"
	"sync"
)

// Store abstracts file system operations so caches can be tested without disk I/O.
type Store interface {
	Read(key string) ([]byte, error)
	Write(key string, data []byte) error
	Delete(key string) error
}

// DiskStore reads and writes files under a directory on the real filesystem.
type DiskStore struct {
	dir string
}

func NewDiskStore(dir string) *DiskStore {
	return &DiskStore{dir: dir}
}

func (d *DiskStore) Read(key string) ([]byte, error) {
	return os.ReadFile(filepath.Join(d.dir, key))
}

func (d *DiskStore) Write(key string, data []byte) error {
	if err := os.MkdirAll(d.dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(d.dir, key), data, 0600)
}

func (d *DiskStore) Delete(key string) error {
	return os.Remove(filepath.Join(d.dir, key))
}

// MemStore is an in-memory Store for tests — no disk I/O.
type MemStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func NewMemStore() *MemStore {
	return &MemStore{data: make(map[string][]byte)}
}

func (m *MemStore) Read(key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.data[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	cp := make([]byte, len(d))
	copy(cp, d)
	return cp, nil
}

func (m *MemStore) Write(key string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	m.data[key] = cp
	return nil
}

func (m *MemStore) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}
