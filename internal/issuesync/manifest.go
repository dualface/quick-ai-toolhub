package issuesync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Manifest struct {
	Version     int                    `json:"version"`
	GeneratedAt string                 `json:"generated_at"`
	Sprints     map[string]IssueRecord `json:"sprints"`
	Tasks       map[string]IssueRecord `json:"tasks"`
}

type IssueRecord struct {
	IssueNumber int    `json:"issue_number"`
	IssueID     int64  `json:"issue_id"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	BodyHash    string `json:"body_hash"`
	Source      string `json:"source"`
	UpdatedAt   string `json:"updated_at"`
}

func LoadManifest(path string) (*Manifest, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Manifest{
				Version: 1,
				Sprints: map[string]IssueRecord{},
				Tasks:   map[string]IssueRecord{},
			}, nil
		}
		return nil, fmt.Errorf("read manifest %s: %w", path, err)
	}

	var manifest Manifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}

	if manifest.Sprints == nil {
		manifest.Sprints = map[string]IssueRecord{}
	}
	if manifest.Tasks == nil {
		manifest.Tasks = map[string]IssueRecord{}
	}
	if manifest.Version == 0 {
		manifest.Version = 1
	}

	return &manifest, nil
}

func (m *Manifest) Save(path string) error {
	m.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	content, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create manifest directory: %w", err)
	}

	if err := os.WriteFile(path, append(content, '\n'), 0o644); err != nil {
		return fmt.Errorf("write manifest %s: %w", path, err)
	}

	return nil
}
