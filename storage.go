package goldga

//go:generate mockgen -source=$GOFILE -package=$GOPACKAGE -destination=storage_mock_test.go Storage

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/spf13/afero"
)

// nolint: gochecknoglobals
var defaultFs = afero.NewCacheOnReadFs(
	afero.NewOsFs(),
	afero.NewMemMapFs(),
	time.Minute,
)

type Storage interface {
	Read() ([]byte, error)
	Write(data []byte) error
}

var _ Storage = (*SingleStorage)(nil)

type SingleStorage struct {
	Path string
	Fs   afero.Fs
}

func (s *SingleStorage) Read() ([]byte, error) {
	data, err := afero.ReadFile(s.Fs, s.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	return data, nil
}

func (s *SingleStorage) Write(data []byte) error {
	if err := s.Fs.MkdirAll(filepath.Dir(s.Path), os.ModePerm); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	if err := afero.WriteFile(s.Fs, s.Path, data, os.ModePerm); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

type suiteData struct {
	Snapshots map[string]string `toml:"snapshots"`
}

func newSuiteData() *suiteData {
	return &suiteData{
		Snapshots: map[string]string{},
	}
}

func (s *suiteData) sortSnapshotKeys() []string {
	keys := make([]string, 0, len(s.Snapshots))

	for k := range s.Snapshots {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}

var _ Storage = (*SuiteStorage)(nil)

type SuiteStorage struct {
	Path string
	Name string
	Fs   afero.Fs
}

func (s *SuiteStorage) getSuiteData() (*suiteData, error) {
	exists, err := afero.Exists(s.Fs, s.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to check file exist: %w", err)
	}

	if !exists {
		return nil, afero.ErrFileNotFound
	}

	file, err := s.Fs.Open(s.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	defer file.Close()

	data := newSuiteData()

	if _, err := toml.DecodeReader(file, &data); err != nil {
		return nil, fmt.Errorf("toml decode error: %w", err)
	}

	return data, nil
}

func (s *SuiteStorage) Read() ([]byte, error) {
	data, err := s.getSuiteData()
	if err != nil {
		return nil, err
	}

	if s, ok := data.Snapshots[s.Name]; ok {
		return []byte(s), nil
	}

	return nil, afero.ErrFileNotFound
}

func (s *SuiteStorage) Write(input []byte) error {
	data, err := s.getSuiteData()
	if err != nil {
		if !errors.Is(err, afero.ErrFileNotFound) {
			return err
		}

		data = newSuiteData()
	}

	data.Snapshots[s.Name] = string(input)

	if err := s.Fs.MkdirAll(filepath.Dir(s.Path), os.ModePerm); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	file, err := s.Fs.Create(s.Path)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	defer file.Close()

	w := bufio.NewWriter(file)
	lines := []string{
		"# Generated by goldga. DO NOT EDIT.",
		"[snapshots]",
	}

	// Print header
	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return fmt.Errorf("header write error: %w", err)
		}
	}

	// Print snapshots
	for _, k := range data.sortSnapshotKeys() {
		v := data.Snapshots[k]

		if _, err := fmt.Fprintf(w, "%q = '''\n%s'''\n", k, v); err != nil {
			return fmt.Errorf("snapshot write error: %w", err)
		}
	}

	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush error: %w", err)
	}

	return nil
}
