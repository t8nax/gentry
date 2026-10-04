package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

func tempPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "state", "state.db")
}

func mustOpen(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenCreatesStore(t *testing.T) {
	path := tempPath(t)
	s := mustOpen(t, path)
	v, err := schemaVersion(s.db)
	if err != nil {
		t.Fatal(err)
	}
	if v != SchemaVersion() || v != 1 {
		t.Errorf("schema version %d, want %d", v, SchemaVersion())
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("store not created: %v", err)
	}
	// Opening an up-to-date store changes nothing and makes no copy.
	s.Close()
	mustOpen(t, path)
	if m, _ := filepath.Glob(path + ".schema-*.bak"); len(m) != 0 {
		t.Errorf("unexpected copies: %v", m)
	}
}

func TestOpenReadMissing(t *testing.T) {
	path := tempPath(t)
	if _, err := OpenRead(path); !errors.Is(err, ErrNotExist) {
		t.Fatalf("got %v, want ErrNotExist", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("reading must not create anything, stat: %v", err)
	}
}

func TestEvents(t *testing.T) {
	path := tempPath(t)
	s := mustOpen(t, path)
	now = func() time.Time { return time.Date(2026, 10, 5, 14, 3, 11, 482_000_000, time.FixedZone("MSK", 3*3600)) }
	t.Cleanup(func() { now = time.Now })

	err := s.Write(func(tx *Tx) error {
		if _, err := tx.AddEvent("task.taken", "shop", "SHOP-7", map[string]string{"scenario": "feature"}); err != nil {
			return err
		}
		_, err := tx.AddEvent("settings.changed", "", "", nil)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	r, err := OpenRead(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	events, err := r.Events(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	e := events[0]
	if e.Seq != 1 || e.Type != "task.taken" || e.Project != "shop" || e.Task != "SHOP-7" || string(e.Data) != `{"scenario":"feature"}` {
		t.Errorf("unexpected event: %+v", e)
	}
	if got := e.Time.Format(TimeFormat); got != "2026-10-05T11:03:11.482Z" {
		t.Errorf("time %s, want UTC with milliseconds", got)
	}
	if e := events[1]; e.Seq != 2 || e.Project != "" || e.Task != "" || string(e.Data) != "{}" {
		t.Errorf("unexpected event: %+v", e)
	}
	if after, _ := r.Events(1); len(after) != 1 || after[0].Seq != 2 {
		t.Errorf("events after 1: %+v", after)
	}
	if after, _ := r.Events(2); len(after) != 0 {
		t.Errorf("events after 2: %+v", after)
	}
}

func TestWriteRollsBack(t *testing.T) {
	s := mustOpen(t, tempPath(t))
	boom := errors.New("boom")
	err := s.Write(func(tx *Tx) error {
		if _, err := tx.AddEvent("task.taken", "shop", "", nil); err != nil {
			return err
		}
		return boom
	})
	if err != boom {
		t.Fatalf("got %v, want the error of fn as is", err)
	}
	if events, _ := s.Events(0); len(events) != 0 {
		t.Errorf("rolled back event was written: %+v", events)
	}
}

func setSchemaVersion(t *testing.T, s *Store, v int) {
	t.Helper()
	if _, err := s.db.Exec(`UPDATE meta SET value = ? WHERE key = 'schema_version'`, strconv.Itoa(v)); err != nil {
		t.Fatal(err)
	}
}

func TestNewerStoreRefused(t *testing.T) {
	path := tempPath(t)
	s := mustOpen(t, path)
	setSchemaVersion(t, s, 3)
	s.Close()

	var ne *NewerError
	if _, err := Open(path); !errors.As(err, &ne) || ne.Schema != 3 || ne.Supported != SchemaVersion() {
		t.Errorf("Open: got %v, want NewerError for schema 3", err)
	}
	if _, err := OpenRead(path); !errors.As(err, &ne) {
		t.Errorf("OpenRead: got %v, want NewerError", err)
	}
}

func TestMigrationKeepsCopy(t *testing.T) {
	path := tempPath(t)
	s := mustOpen(t, path)
	s.Write(func(tx *Tx) error {
		_, err := tx.AddEvent("task.taken", "shop", "", nil)
		return err
	})
	s.Close()

	orig := migrations
	migrations = append(append([]string(nil), orig...), `CREATE TABLE extra (x TEXT);`)
	t.Cleanup(func() { migrations = orig })

	s = mustOpen(t, path)
	if v, _ := schemaVersion(s.db); v != 2 {
		t.Errorf("schema version %d, want 2", v)
	}
	if _, err := s.db.Exec(`INSERT INTO extra (x) VALUES ('ok')`); err != nil {
		t.Errorf("migration not applied: %v", err)
	}
	if events, _ := s.Events(0); len(events) != 1 {
		t.Errorf("events lost in migration: %+v", events)
	}

	migrations = orig
	c, err := OpenRead(path + ".schema-1.bak")
	if err != nil {
		t.Fatalf("copy before migration: %v", err)
	}
	defer c.Close()
	if v, _ := schemaVersion(c.db); v != 1 {
		t.Errorf("copy has schema %d, want 1", v)
	}
	if events, _ := c.Events(0); len(events) != 1 {
		t.Errorf("copy has %d events, want 1", len(events))
	}
}

// Concurrent writes: writers processes append events to one store at once.
const (
	writers          = 8
	eventsPerWriter  = 200
	writerEnvPath    = "GENTRY_TEST_WRITER_PATH"
	writerEnvID      = "GENTRY_TEST_WRITER_ID"
	writerEventsType = "test.written"
)

type written struct {
	Writer int `json:"writer"`
	N      int `json:"n"`
}

// TestHelperWriter is not a test: it is the writer process of
// TestConcurrentWrites.
func TestHelperWriter(t *testing.T) {
	path := os.Getenv(writerEnvPath)
	if path == "" {
		t.Skip("writer process of TestConcurrentWrites")
	}
	id, _ := strconv.Atoi(os.Getenv(writerEnvID))
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for n := range eventsPerWriter {
		err := s.Write(func(tx *Tx) error {
			_, err := tx.AddEvent(writerEventsType, "shop", "", written{Writer: id, N: n})
			return err
		})
		if err != nil {
			t.Fatalf("writer %d, event %d: %v", id, n, err)
		}
	}
}

func TestConcurrentWrites(t *testing.T) {
	path := tempPath(t)
	var wg sync.WaitGroup
	errs := make([]error, writers)
	outs := make([][]byte, writers)
	start := time.Now()
	for id := range writers {
		wg.Go(func() {
			cmd := exec.Command(os.Args[0], "-test.run=^TestHelperWriter$", "-test.count=1")
			cmd.Env = append(os.Environ(), writerEnvPath+"="+path, fmt.Sprintf("%s=%d", writerEnvID, id))
			outs[id], errs[id] = cmd.CombinedOutput()
		})
	}
	wg.Wait()
	for id, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v\n%s", id, err, outs[id])
		}
	}
	t.Logf("%d processes wrote %d events in %v", writers, writers*eventsPerWriter, time.Since(start).Round(time.Millisecond))

	s, err := OpenRead(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	events, err := s.Events(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != writers*eventsPerWriter {
		t.Fatalf("got %d events, want %d", len(events), writers*eventsPerWriter)
	}
	seen := map[written]bool{}
	for i, e := range events {
		if e.Seq != int64(i+1) {
			t.Fatalf("event %d has number %d: numbers must go without gaps", i+1, e.Seq)
		}
		var w written
		if err := json.Unmarshal(e.Data, &w); err != nil {
			t.Fatal(err)
		}
		if seen[w] {
			t.Errorf("event %+v written twice", w)
		}
		seen[w] = true
	}
}
