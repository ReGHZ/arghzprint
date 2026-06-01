package template

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Store manages template files on disk.
// Each job type maps to a <type>.html file in the templates directory.
// Type names are lowercased for file lookup: "KITCHEN" → "kitchen.html".
type Store struct {
	dir string
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create template dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Get returns the template HTML for the given job type.
// Returns ErrNotFound if no template file exists for that type.
func (s *Store) Get(jobType string) (string, error) {
	data, err := os.ReadFile(s.path(jobType))
	if os.IsNotExist(err) {
		return "", fmt.Errorf("%w: %s", ErrNotFound, jobType)
	}
	if err != nil {
		return "", fmt.Errorf("read template %s: %w", jobType, err)
	}
	return string(data), nil
}

// Save writes template HTML for the given job type.
func (s *Store) Save(jobType, html string) error {
	if err := os.WriteFile(s.path(jobType), []byte(html), 0644); err != nil {
		return fmt.Errorf("write template %s: %w", jobType, err)
	}
	return nil
}

// List returns all job type names that have a template file.
func (s *Store) List() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}

	var types []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".html" {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".html")
		types = append(types, strings.ToUpper(name))
	}
	return types, nil
}

// Delete removes the template file for the given job type.
func (s *Store) Delete(jobType string) error {
	err := os.Remove(s.path(jobType))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *Store) Dir() string {
	return s.dir
}

func (s *Store) path(jobType string) string {
	return filepath.Join(s.dir, strings.ToLower(jobType)+".html")
}

var ErrNotFound = fmt.Errorf("template not found")
