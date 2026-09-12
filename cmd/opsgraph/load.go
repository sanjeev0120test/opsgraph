package main

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sanjeev0120test/opsgraph/internal/ask"
	"github.com/sanjeev0120test/opsgraph/internal/config"
	"github.com/sanjeev0120test/opsgraph/internal/model"
	"github.com/sanjeev0120test/opsgraph/internal/store"
)

// sourceFlags are the common --fixture/--config/--data-dir trio.
type sourceFlags struct {
	fixture    string
	configPath string
	dataDir    string
}

func (f *sourceFlags) loadCtx(ctx context.Context, since time.Duration) (*loadedStore, *config.Config, error) {
	if err := validSince(since); err != nil {
		return nil, nil, err
	}
	fixture, err := resolveFixtureExclusive(f.fixture, f.dataDir)
	if err != nil {
		return nil, nil, err
	}
	cfgPath := configPathOrEnv(f.configPath)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, nil, err
	}
	if since == 0 {
		since = cfg.Since()
	}
	ls, err := loadAskStore(ctx, fixture, cfgPath, f.dataDir, cfg, since)
	return ls, cfg, err
}

// resolveFixtureExclusive resolves --fixture / OPSGRAPH_FIXTURE and rejects
// combinations with --data-dir / OPSGRAPH_DATA_DIR (flag or env).
func resolveFixtureExclusive(fixtureFlag, dataDirFlag string) (string, error) {
	fixture := strings.TrimSpace(fixtureFlag)
	if fixture == "" {
		fixture = strings.TrimSpace(os.Getenv("OPSGRAPH_FIXTURE"))
	}
	dataDir := strings.TrimSpace(dataDirFlag)
	if dataDir == "" {
		dataDir = strings.TrimSpace(os.Getenv("OPSGRAPH_DATA_DIR"))
	}
	if fixture != "" && dataDir != "" {
		return "", fail(2, "--fixture and --data-dir are mutually exclusive")
	}
	return fixture, nil
}

// failSource maps store/config load errors to CLI exit codes.
func failSource(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrEmptyStore) {
		return fail(1, "%v", err)
	}
	return fail(2, "%v", err)
}

func askService(ls *loadedStore, query string, since time.Duration) (model.AskResult, error) {
	if since == 0 {
		since = config.DefaultSince
	}
	return ask.Ask(ls.store, query, ask.Options{Since: since, Now: ls.now, WithRunbook: true})
}

// failAsk maps ask errors to CLI exit codes (1 = not found / ambiguous, 2 = other).
func failAsk(err error) error {
	return failAskStore(nil, err)
}

func failAskStore(s *store.Store, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ask.ErrServiceNotFound) {
		q := queryFromQuotedErr(err)
		return failLookupNames(q, store.ErrNotFound, namesFromStore(s))
	}
	if errors.Is(err, store.ErrAmbiguous) {
		return fail(1, "%v", err)
	}
	return fail(2, "%v", err)
}

// failLookup maps store service lookup errors (not found / ambiguous -> 1).
func failLookup(query string, err error) error {
	return failLookupNames(query, err, nil)
}

func failLookupStore(s *store.Store, query string, err error) error {
	return failLookupNames(query, err, namesFromStore(s))
}

func failLookupNames(query string, err error, names []string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, store.ErrNotFound) {
		if hint := suggestServices(query, names); len(hint) > 0 {
			return fail(1, "service %q not found (did you mean %s?  try: opsgraph services)", query, strings.Join(hint, ", "))
		}
		return fail(1, "service %q not found (try: opsgraph services)", query)
	}
	if errors.Is(err, store.ErrAmbiguous) {
		return fail(1, "%v", err)
	}
	return fail(2, "%v", err)
}

func namesFromStore(s *store.Store) []string {
	if s == nil {
		return nil
	}
	svcs, err := s.ListServices()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(svcs)*2)
	for _, svc := range svcs {
		if model.IsNoiseForPaging(svc) {
			continue
		}
		out = append(out, svc.ID, svc.Name)
		out = append(out, svc.Aliases...)
	}
	return out
}

func queryFromQuotedErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	i := strings.Index(s, `"`)
	if i < 0 || i+1 >= len(s) {
		return ""
	}
	j := strings.LastIndex(s, `"`)
	if j <= i {
		return ""
	}
	return s[i+1 : j]
}

func suggestServices(query string, names []string) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" || len(names) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var hits []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		low := strings.ToLower(id)
		if id == "" || low == q || seen[low] {
			return
		}
		seen[low] = true
		hits = append(hits, id)
	}
	for _, n := range names {
		low := strings.ToLower(strings.TrimSpace(n))
		if low == "" {
			continue
		}
		if strings.Contains(low, q) || strings.Contains(q, low) {
			add(n)
		}
	}
	if len(hits) == 0 {
		for _, n := range names {
			low := strings.ToLower(strings.TrimSpace(n))
			if low == "" {
				continue
			}
			if editDistance(low, q) <= 2 && absInt(utf8.RuneCountInString(low)-utf8.RuneCountInString(q)) <= 2 {
				add(n)
			}
		}
	}
	sort.Strings(hits)
	if len(hits) > 3 {
		hits = hits[:3]
	}
	return hits
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func editDistance(a, b string) int {
	if a == b {
		return 0
	}
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	if len(ra) > 32 || len(rb) > 32 {
		return 99
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 0
			if ra[i-1] != rb[j-1] {
				cost = 1
			}
			del, ins, sub := prev[j]+1, cur[j-1]+1, prev[j-1]+cost
			if ins < del {
				del = ins
			}
			if sub < del {
				del = sub
			}
			cur[j] = del
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
