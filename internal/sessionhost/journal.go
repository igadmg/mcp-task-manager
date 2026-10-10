package sessionhost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/sessionapi"
)

const eventsFile = "events.jsonl"

// Journal is the per-session source of truth: a JSONL event log with
// per-session seq starting at 1. Subscribers replay from disk, so a slow
// consumer never loses events (design §5).
type Journal struct {
	mu      sync.Mutex
	dir     string
	f       *os.File
	lastSeq int64
	subs    map[*Subscription]struct{}
	closed  bool
}

// OpenJournal opens (or creates) events.jsonl inside dir and recovers
// lastSeq from existing content.
func OpenJournal(dir string) (*Journal, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, eventsFile), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	j := &Journal{dir: dir, f: f, subs: make(map[*Subscription]struct{})}
	if err := j.recoverLastSeq(); err != nil {
		f.Close()
		return nil, err
	}
	return j, nil
}

func (j *Journal) recoverLastSeq() error {
	events, err := j.readAll()
	if err != nil {
		return err
	}
	for _, ev := range events {
		if ev.Seq > j.lastSeq {
			j.lastSeq = ev.Seq
		}
	}
	return nil
}

// Append assigns seq and ts, writes the event, and fans it out to
// subscribers. Slow subscribers are dropped (they re-read from disk).
func (j *Journal) Append(ev sessionapi.Event) (sessionapi.Event, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return ev, errJournalClosed
	}
	j.lastSeq++
	ev.Seq = j.lastSeq
	ev.Ts = time.Now().UTC()
	line, err := json.Marshal(ev)
	if err != nil {
		return ev, err
	}
	if _, err := j.f.Write(append(line, '\n')); err != nil {
		return ev, err
	}
	for sub := range j.subs {
		select {
		case sub.live <- ev:
		default:
			// Slow subscriber: drop it; it will reconnect via Last-Event-ID.
			j.dropLocked(sub)
		}
	}
	return ev, nil
}

// dropLocked removes the subscriber and closes its channels; callers must
// hold j.mu so no Append can be sending into sub.live concurrently.
func (j *Journal) dropLocked(sub *Subscription) {
	delete(j.subs, sub)
	sub.finish()
}

// ReadAfter returns all events with seq > after, read from disk.
func (j *Journal) ReadAfter(after int64) ([]sessionapi.Event, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.readAllAfter(after)
}

func (j *Journal) readAll() ([]sessionapi.Event, error) {
	return j.readAllAfter(0)
}

func (j *Journal) readAllAfter(after int64) ([]sessionapi.Event, error) {
	data, err := os.ReadFile(filepath.Join(j.dir, eventsFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var events []sessionapi.Event
	for _, line := range splitLines(data) {
		if len(line) == 0 {
			continue
		}
		var ev sessionapi.Event
		if err := json.Unmarshal(line, &ev); err != nil {
			return nil, err
		}
		if ev.Seq > after {
			events = append(events, ev)
		}
	}
	return events, nil
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, trimCR(data[start:i]))
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, trimCR(data[start:]))
	}
	return lines
}

func trimCR(line []byte) []byte {
	for len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	return line
}

// Subscribe replays events with seq > after, then streams live events.
func (j *Journal) Subscribe(after int64) (*Subscription, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return nil, errJournalClosed
	}
	replay, err := j.readAllAfter(after)
	if err != nil {
		return nil, err
	}
	sub := newSubscription(j)
	j.subs[sub] = struct{}{}
	go sub.deliver(replay)
	return sub, nil
}

// Close shuts the journal down; subscribers see their channels closed.
func (j *Journal) Close() error {
	j.mu.Lock()
	if j.closed {
		j.mu.Unlock()
		return nil
	}
	j.closed = true
	err := j.f.Close()
	subs := make([]*Subscription, 0, len(j.subs))
	for sub := range j.subs {
		subs = append(subs, sub)
		delete(j.subs, sub)
	}
	j.mu.Unlock()
	// Subscribers are already detached from the fan-out map, so finishing
	// them here cannot race with an in-flight Append.
	for _, sub := range subs {
		sub.finish()
	}
	return err
}

var errJournalClosed = &sessionapi.Error{Code: sessionapi.ErrCodeConflict, Message: "journal closed"}

type Subscription struct {
	// C receives replayed then live events.
	C    <-chan sessionapi.Event
	ch   chan sessionapi.Event
	live chan sessionapi.Event
	done chan struct{}

	j    *Journal
	once sync.Once
}

func newSubscription(j *Journal) *Subscription {
	sub := &Subscription{
		ch:   make(chan sessionapi.Event, 256),
		live: make(chan sessionapi.Event, 64),
		done: make(chan struct{}),
		j:    j,
	}
	sub.C = sub.ch
	return sub
}

// deliver pushes the replay first, then forwards live events; the two-phase
// design keeps ordering (live events cannot overtake the replay).
func (s *Subscription) deliver(replay []sessionapi.Event) {
	defer close(s.ch)
	for _, ev := range replay {
		select {
		case s.ch <- ev:
		case <-s.done:
			return
		}
	}
	for {
		select {
		case ev, ok := <-s.live:
			if !ok {
				return
			}
			select {
			case s.ch <- ev:
			case <-s.done:
				return
			}
		case <-s.done:
			return
		}
	}
}

// finish closes the channels; the Subscription must already be detached
// from the journal fan-out map.
func (s *Subscription) finish() {
	s.once.Do(func() {
		close(s.done)
		close(s.live)
	})
}

// Close terminates the Subscription.
func (s *Subscription) Close() {
	s.j.mu.Lock()
	delete(s.j.subs, s)
	s.j.mu.Unlock()
	s.finish()
}

// Meta is the session record persisted in meta.json (design §5).
type Meta struct {
	ID            string            `json:"id"`
	Title         string            `json:"title"`
	Status        sessionapi.Status `json:"status"`
	WorkspacePath string            `json:"workspace_path"`
	TaskID        string            `json:"task_id,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
	LastSeq       int64             `json:"last_seq"`
}

// WriteMeta atomically rewrites meta.json (temp file + rename).
func WriteMeta(dir string, m Meta) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "meta.json.tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "meta.json"))
}

// ReadMeta loads meta.json from dir.
func ReadMeta(dir string) (Meta, error) {
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return Meta{}, err
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return Meta{}, err
	}
	return m, nil
}
