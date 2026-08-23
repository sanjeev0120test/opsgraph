package ingest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/model"
	"github.com/sanjeev0120test/opsgraph/internal/store"
	"gopkg.in/yaml.v3"
)

var rolloutReadyRe = regexp.MustCompile(`^rollout (\S+) \((\d+)/(\d+) ready\)`)

// WritePack serializes the store into a portable fixture directory that
// `opsgraph test` / `ask --fixture` can replay bit-identically on any OS.
func WritePack(s *store.Store, now time.Time, dir string) error {
	if s == nil {
		return fmt.Errorf("store is nil")
	}
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("pack directory is empty")
	}
	if err := os.MkdirAll(filepath.Join(dir, "k8s"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "runbooks"), 0o755); err != nil {
		return err
	}
	now = now.UTC().Truncate(time.Second)
	if err := writeYAMLFile(filepath.Join(dir, "meta.yaml"), metaFile{Now: now}); err != nil {
		return err
	}

	svcs, err := s.ListServices()
	if err != nil {
		return err
	}
	sort.Slice(svcs, func(i, j int) bool { return svcs[i].ID < svcs[j].ID })
	fxs := fxServices{Services: make([]fxService, 0, len(svcs))}
	for _, v := range svcs {
		fxs.Services = append(fxs.Services, fxService{
			ID: v.ID, Name: v.Name, Aliases: v.Aliases, OwnerID: v.OwnerID,
			Health: v.Health, Labels: yamlStringMap(v.Labels), Sources: v.Sources,
		})
	}
	if err := writeYAMLFile(filepath.Join(dir, "services.yaml"), fxs); err != nil {
		return err
	}

	owners, err := s.ListOwners()
	if err != nil {
		return err
	}
	fxo := fxOwners{Owners: make([]fxOwner, 0, len(owners))}
	for _, o := range owners {
		fxo.Owners = append(fxo.Owners, fxOwner{ID: o.ID, Name: o.Name, Team: o.Team, Email: o.Email})
	}
	if err := writeYAMLFile(filepath.Join(dir, "owners.yaml"), fxo); err != nil {
		return err
	}

	changes, err := s.ListAllChanges()
	if err != nil {
		return err
	}
	fxc := fxChanges{Changes: make([]fxChange, 0, len(changes))}
	for _, c := range changes {
		fxc.Changes = append(fxc.Changes, fxChange{
			ID: c.ID, ServiceID: c.ServiceID, At: c.At.UTC(), Type: c.Type,
			Summary: c.Summary, Author: c.Author, Revision: c.Revision,
			Source: c.Source, EvidenceID: c.EvidenceID,
		})
	}
	sort.Slice(fxc.Changes, func(i, j int) bool { return fxc.Changes[i].ID < fxc.Changes[j].ID })
	if err := writeYAMLFile(filepath.Join(dir, "changes.yaml"), fxc); err != nil {
		return err
	}

	deps, err := s.ListAllDependencies()
	if err != nil {
		return err
	}
	fxd := fxDeps{Dependencies: make([]fxDep, 0, len(deps))}
	for _, d := range deps {
		fxd.Dependencies = append(fxd.Dependencies, fxDep{
			From: d.FromServiceID, To: d.ToServiceID, Type: d.Type, Source: d.Source,
		})
	}
	sort.Slice(fxd.Dependencies, func(i, j int) bool {
		if fxd.Dependencies[i].From != fxd.Dependencies[j].From {
			return fxd.Dependencies[i].From < fxd.Dependencies[j].From
		}
		return fxd.Dependencies[i].To < fxd.Dependencies[j].To
	})
	if err := writeYAMLFile(filepath.Join(dir, "dependencies.yaml"), fxd); err != nil {
		return err
	}

	alerts, err := s.ListAllAlerts()
	if err != nil {
		return err
	}
	fxa := fxAlerts{Alerts: make([]fxAlert, 0, len(alerts))}
	for _, a := range alerts {
		fxa.Alerts = append(fxa.Alerts, fxAlert{
			ID: a.ID, ServiceID: a.ServiceID, At: a.At.UTC(), Severity: a.Severity,
			Name: a.Name, Status: a.Status, Summary: a.Summary, Source: a.Source,
			EvidenceID: a.EvidenceID,
		})
	}
	sort.Slice(fxa.Alerts, func(i, j int) bool { return fxa.Alerts[i].ID < fxa.Alerts[j].ID })
	if err := writeYAMLFile(filepath.Join(dir, "alerts.yaml"), fxa); err != nil {
		return err
	}

	k8sDeps, k8sEvs, err := packK8s(s, svcs, changes)
	if err != nil {
		return err
	}
	if err := writeYAMLFile(filepath.Join(dir, "k8s", "deployments.yaml"), k8sDeps); err != nil {
		return err
	}
	if err := writeYAMLFile(filepath.Join(dir, "k8s", "events.yaml"), k8sEvs); err != nil {
		return err
	}

	for _, svc := range svcs {
		rb, err := s.GetRunbook(svc.ID)
		if err != nil {
			continue
		}
		name := slug(svc.ID) + ".md"
		body := renderPackedRunbook(*rb)
		if err := os.WriteFile(filepath.Join(dir, "runbooks", name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func packK8s(s *store.Store, svcs []model.Service, changes []model.Change) (k8sDeployments, k8sEvents, error) {
	rollout := map[string]model.Change{}
	for _, c := range changes {
		if c.Type != "rollout" {
			continue
		}
		prev, ok := rollout[c.ServiceID]
		if !ok || c.At.After(prev.At) {
			rollout[c.ServiceID] = c
		}
	}
	var deps k8sDeployments
	for _, svc := range svcs {
		_, hasRollout := rollout[svc.ID]
		if !hasSource(svc.Sources, "kubernetes") && !hasRollout {
			continue
		}
		d := k8sDeployment{
			Name: svc.ID, Namespace: "default", ServiceID: svc.ID,
		}
		switch svc.Health {
		case model.HealthHealthy:
			d.Desired, d.Ready = 1, 1
		case model.HealthDegraded:
			d.Desired, d.Ready = 2, 1
		case model.HealthUnhealthy:
			d.Desired, d.Ready = 1, 0
		default:
			d.Desired, d.Ready = 0, 0
		}
		if c, ok := rollout[svc.ID]; ok {
			d.UpdatedAt = c.At.UTC()
			if name, ready, desired, ok := parseRolloutReady(c.Summary); ok {
				d.Name = name
				d.Ready = ready
				d.Desired = desired
			}
		}
		deps.Deployments = append(deps.Deployments, d)
	}
	sort.Slice(deps.Deployments, func(i, j int) bool {
		return deps.Deployments[i].Name < deps.Deployments[j].Name
	})

	evsAll, err := s.ListAllEvidence()
	if err != nil {
		return deps, k8sEvents{}, err
	}
	var evs k8sEvents
	for _, e := range evsAll {
		if e.Kind != "k8s-event" {
			continue
		}
		reason, msg := splitEventSummary(e.Summary)
		evs.Events = append(evs.Events, k8sEvent{
			ServiceID: e.ServiceID, At: e.At.UTC(), Reason: reason,
			Message: msg, Type: "Warning", EvidenceID: e.ID,
		})
	}
	sort.Slice(evs.Events, func(i, j int) bool {
		if evs.Events[i].At.Equal(evs.Events[j].At) {
			return evs.Events[i].EvidenceID < evs.Events[j].EvidenceID
		}
		return evs.Events[i].At.Before(evs.Events[j].At)
	})
	return deps, evs, nil
}

func parseRolloutReady(summary string) (name string, ready, desired int, ok bool) {
	m := rolloutReadyRe.FindStringSubmatch(strings.TrimSpace(summary))
	if len(m) != 4 {
		return "", 0, 0, false
	}
	r, err1 := strconv.Atoi(m[2])
	d, err2 := strconv.Atoi(m[3])
	if err1 != nil || err2 != nil {
		return "", 0, 0, false
	}
	return m[1], r, d, true
}

func splitEventSummary(summary string) (reason, message string) {
	reason, message, found := strings.Cut(summary, ": ")
	if !found {
		return "", summary
	}
	return reason, message
}

func renderPackedRunbook(rb model.Runbook) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("service: " + rb.ServiceID + "\n")
	if rb.OwnerID != "" {
		b.WriteString("owner: " + rb.OwnerID + "\n")
	}
	b.WriteString("---\n\n")
	for _, st := range rb.Steps {
		fmt.Fprintf(&b, "%d. %s\n", st.Number, st.Text)
		if strings.TrimSpace(st.Check) != "" {
			fmt.Fprintf(&b, "<!-- opsgraph:check=%s -->\n", st.Check)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func writeYAMLFile(path string, v any) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	return os.WriteFile(path, data, 0o644)
}
