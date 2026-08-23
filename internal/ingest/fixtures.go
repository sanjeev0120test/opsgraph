package ingest

import (
	"errors"
	"io/fs"
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/model"
	"github.com/sanjeev0120test/opsgraph/internal/store"
)

// ensureServiceStub inserts a placeholder service for dependency endpoints that
// have no Service row yet (e.g. fixture redis). Existing rows are left alone.
func ensureServiceStub(s *store.Store, id string) error {
	if id == "" {
		return nil
	}
	if _, err := s.GetService(id); err == nil {
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return s.UpsertService(model.Service{
		ID: id, Name: id, Health: model.HealthUnknown, Sources: []string{"dependency"},
	})
}

// --- fixture file schemas (yaml-tagged, converted to model types) ---

type fxServices struct {
	Services []fxService `yaml:"services"`
}

type fxService struct {
	ID      string        `yaml:"id"`
	Name    string        `yaml:"name"`
	Aliases []string      `yaml:"aliases"`
	OwnerID string        `yaml:"owner_id"`
	Health  string        `yaml:"health"`
	Labels  yamlStringMap `yaml:"labels"`
	Sources []string      `yaml:"sources"`
}

type fxOwners struct {
	Owners []fxOwner `yaml:"owners"`
}

type fxOwner struct {
	ID    string `yaml:"id"`
	Name  string `yaml:"name"`
	Team  string `yaml:"team"`
	Email string `yaml:"email"`
}

type fxChanges struct {
	Changes []fxChange `yaml:"changes"`
}

type fxChange struct {
	ID         string    `yaml:"id"`
	ServiceID  string    `yaml:"service_id"`
	At         time.Time `yaml:"at"`
	Type       string    `yaml:"type"`
	Summary    string    `yaml:"summary"`
	Author     string    `yaml:"author"`
	Revision   string    `yaml:"revision"`
	Source     string    `yaml:"source"`
	EvidenceID string    `yaml:"evidence_id"`
}

type fxDeps struct {
	Dependencies []fxDep `yaml:"dependencies"`
}

type fxDep struct {
	From   string `yaml:"from"`
	To     string `yaml:"to"`
	Type   string `yaml:"type"`
	Source string `yaml:"source"`
}

type fxAlerts struct {
	Alerts []fxAlert `yaml:"alerts"`
}

type fxAlert struct {
	ID         string    `yaml:"id"`
	ServiceID  string    `yaml:"service_id"`
	At         time.Time `yaml:"at"`
	Severity   string    `yaml:"severity"`
	Name       string    `yaml:"name"`
	Status     string    `yaml:"status"`
	Summary    string    `yaml:"summary"`
	Source     string    `yaml:"source"`
	EvidenceID string    `yaml:"evidence_id"`
}

// ingestEntities parses and upserts services, owners, changes, dependencies,
// alerts, runbooks, and their evidence.
func ingestEntities(s *store.Store, fsys fs.FS) error {
	if err := ingestServices(s, fsys); err != nil {
		return err
	}
	if err := ingestOwners(s, fsys); err != nil {
		return err
	}
	if err := ingestChanges(s, fsys); err != nil {
		return err
	}
	if err := ingestDeps(s, fsys); err != nil {
		return err
	}
	if err := ingestAlerts(s, fsys); err != nil {
		return err
	}
	if err := ingestRunbooks(s, fsys); err != nil {
		return err
	}
	return nil
}

func ingestServices(s *store.Store, fsys fs.FS) error {
	var f fxServices
	if _, err := readYAML(fsys, "services.yaml", &f); err != nil {
		return err
	}
	for _, v := range f.Services {
		health := v.Health
		if health == "" {
			health = model.HealthUnknown
		}
		if err := s.UpsertService(model.Service{
			ID: v.ID, Name: v.Name, Aliases: v.Aliases, OwnerID: v.OwnerID,
			Health: health, Labels: map[string]string(v.Labels), Sources: v.Sources,
		}); err != nil {
			return err
		}
	}
	return nil
}

func ingestOwners(s *store.Store, fsys fs.FS) error {
	var f fxOwners
	if _, err := readYAML(fsys, "owners.yaml", &f); err != nil {
		return err
	}
	for _, v := range f.Owners {
		if err := s.UpsertOwner(model.Owner{ID: v.ID, Name: v.Name, Team: v.Team, Email: v.Email}); err != nil {
			return err
		}
	}
	return nil
}

func ingestChanges(s *store.Store, fsys fs.FS) error {
	var f fxChanges
	if _, err := readYAML(fsys, "changes.yaml", &f); err != nil {
		return err
	}
	for _, v := range f.Changes {
		if v.Source == "" {
			v.Source = "fixture"
		}
		if err := s.UpsertChange(model.Change{
			ID: v.ID, ServiceID: v.ServiceID, At: v.At, Type: v.Type, Summary: v.Summary,
			Author: v.Author, Revision: v.Revision, Source: v.Source, EvidenceID: v.EvidenceID,
		}); err != nil {
			return err
		}
		if v.EvidenceID != "" {
			if err := s.UpsertEvidence(model.Evidence{
				ID: v.EvidenceID, Source: v.Source, At: v.At, Kind: "change",
				Summary: v.Summary, RawRef: v.Revision, ServiceID: v.ServiceID,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func ingestDeps(s *store.Store, fsys fs.FS) error {
	var f fxDeps
	if _, err := readYAML(fsys, "dependencies.yaml", &f); err != nil {
		return err
	}
	for _, v := range f.Dependencies {
		src := v.Source
		if src == "" {
			src = "fixture"
		}
		for _, id := range []string{v.From, v.To} {
			if err := ensureServiceStub(s, id); err != nil {
				return err
			}
		}
		if err := s.UpsertDependency(model.Dependency{
			FromServiceID: v.From, ToServiceID: v.To, Type: v.Type, Source: src,
		}); err != nil {
			return err
		}
	}
	return nil
}

func ingestAlerts(s *store.Store, fsys fs.FS) error {
	var f fxAlerts
	if _, err := readYAML(fsys, "alerts.yaml", &f); err != nil {
		return err
	}
	for _, v := range f.Alerts {
		if v.Source == "" {
			v.Source = "fixture"
		}
		if err := s.UpsertAlert(model.Alert{
			ID: v.ID, ServiceID: v.ServiceID, At: v.At, Severity: v.Severity, Name: v.Name,
			Status: v.Status, Summary: v.Summary, Source: v.Source, EvidenceID: v.EvidenceID,
		}); err != nil {
			return err
		}
		if v.EvidenceID != "" {
			if err := s.UpsertEvidence(model.Evidence{
				ID: v.EvidenceID, Source: v.Source, At: v.At, Kind: "alert", Summary: v.Summary, ServiceID: v.ServiceID,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}
