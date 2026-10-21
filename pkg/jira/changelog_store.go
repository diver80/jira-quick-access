package jira

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

const (
	changeRetention       = 30 * 24 * time.Hour
	maxEntriesPerInstance = 500
)

// ChangeLog keeps the changelog entries of all instances on disk next to the
// configuration (changelog.json). It is safe for concurrent use.
type ChangeLog struct {
	mu      sync.Mutex
	saveMu  sync.Mutex // keeps an older snapshot from replacing a newer one on disk
	path    string
	data    changeLogFile
	version uint64 // bumped on every change, for caches of Entries/Unread
}

type changeLogFile struct {
	Version   int                           `json:"version"`
	Instances map[string]*instanceChangeLog `json:"instances"`
}

type instanceChangeLog struct {
	Watermark time.Time `json:"watermark"` // "since" of the next changes check
	// Backfilling is set while the first check's look-back has not been completed
	// (it hit the request cap); its continuation is history, not news, too.
	Backfilling bool          `json:"backfilling,omitempty"`
	ReadAt      time.Time     `json:"read_at"` // entries that arrived up to here count as read
	Entries     []ChangeEntry `json:"entries"` // newest first
}

// LoadChangeLog opens the changelog stored next to the configuration file.
func LoadChangeLog() *ChangeLog {
	return OpenChangeLog(filepath.Join(filepath.Dir(getConfigFilePath()), "changelog.json"))
}

// OpenChangeLog opens the changelog at path; a missing or corrupt file starts empty.
func OpenChangeLog(path string) *ChangeLog {
	l := &ChangeLog{path: path}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &l.data)
	}
	if l.data.Instances == nil {
		l.data.Instances = map[string]*instanceChangeLog{}
	}
	return l
}

func (l *ChangeLog) instanceLocked(id string) *instanceChangeLog {
	il := l.data.Instances[id]
	if il == nil {
		il = &instanceChangeLog{}
		l.data.Instances[id] = il
	}
	return il
}

// Version changes whenever entries or read marks change.
func (l *ChangeLog) Version() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.version
}

// Watermarks returns the "since" value of the next changes check per instance.
func (l *ChangeLog) Watermarks() map[string]time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]time.Time, len(l.data.Instances))
	for id, il := range l.data.Instances {
		out[id] = il.Watermark
	}
	return out
}

// Merge stores the result of a changes check and returns the entries that were not
// known yet. history reports that they belong to the initial look-back (the first
// check or its continuation): they are marked read and must not be announced.
func (l *ChangeLog) Merge(instID string, ch InstanceChanges, now time.Time) (added []ChangeEntry, history bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	il := l.instanceLocked(instID)
	history = ch.Backfill || il.Backfilling
	added = l.addLocked(il, instID, ch.Entries, now)
	if !ch.Watermark.IsZero() {
		il.Watermark = ch.Watermark
	}
	if history {
		il.Backfilling = !ch.Complete
		il.ReadAt = now
	}
	return added, history
}

// Add stores entries without touching the watermark (e.g. filter membership changes).
func (l *ChangeLog) Add(instID string, entries []ChangeEntry, now time.Time) []ChangeEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.addLocked(l.instanceLocked(instID), instID, entries, now)
}

func (l *ChangeLog) addLocked(il *instanceChangeLog, instID string, entries []ChangeEntry, now time.Time) []ChangeEntry {
	known := make(map[string]bool, len(il.Entries))
	for _, e := range il.Entries {
		known[e.ID] = true
	}
	var added []ChangeEntry
	for _, e := range entries {
		if e.ID == "" || known[e.ID] || now.Sub(e.At) > changeRetention {
			continue
		}
		known[e.ID] = true
		e.InstanceID = instID
		e.Arrived = now
		added = append(added, e)
	}
	il.Entries = append(il.Entries, added...)
	sortNewestFirst(il.Entries)
	il.Entries = pruneEntries(il.Entries, now)
	sortNewestFirst(added)
	l.version++
	return added
}

func sortNewestFirst(entries []ChangeEntry) {
	slices.SortStableFunc(entries, func(a, b ChangeEntry) int { return b.At.Compare(a.At) })
}

func pruneEntries(entries []ChangeEntry, now time.Time) []ChangeEntry {
	if len(entries) > maxEntriesPerInstance {
		entries = entries[:maxEntriesPerInstance]
	}
	for len(entries) > 0 && now.Sub(entries[len(entries)-1].At) > changeRetention {
		entries = entries[:len(entries)-1]
	}
	return entries
}

// Entries returns the entries of the given instances (all when ids is empty), newest first.
func (l *ChangeLog) Entries(ids ...string) []ChangeEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []ChangeEntry
	for id, il := range l.data.Instances {
		if len(ids) == 0 || slices.Contains(ids, id) {
			out = append(out, il.Entries...)
		}
	}
	sortNewestFirst(out)
	return out
}

// ReadAt returns per instance up to when arrived entries count as read.
func (l *ChangeLog) ReadAt() map[string]time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]time.Time, len(l.data.Instances))
	for id, il := range l.data.Instances {
		out[id] = il.ReadAt
	}
	return out
}

// IsUnread reports whether an entry arrived after the read mark.
func IsUnread(e ChangeEntry, readAt time.Time) bool {
	return e.Arrived.After(readAt)
}

// Unread counts entries of the given instances (all when ids is empty) that arrived
// after their read mark. When skipOwn is set, the user's own changes are not counted.
func (l *ChangeLog) Unread(skipOwn bool, ids ...string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for id, il := range l.data.Instances {
		if len(ids) > 0 && !slices.Contains(ids, id) {
			continue
		}
		for _, e := range il.Entries {
			if IsUnread(e, il.ReadAt) && !(skipOwn && e.Own) {
				n++
			}
		}
	}
	return n
}

// MarkRead marks all entries of the given instances (all when ids is empty) that
// arrived up to t as read.
func (l *ChangeLog) MarkRead(t time.Time, ids ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for id, il := range l.data.Instances {
		if (len(ids) == 0 || slices.Contains(ids, id)) && t.After(il.ReadAt) {
			il.ReadAt = t
			l.version++
		}
	}
}

// Retain drops instances that are no longer configured or have the changelog off.
func (l *ChangeLog) Retain(ids []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for id := range l.data.Instances {
		if !slices.Contains(ids, id) {
			delete(l.data.Instances, id)
			l.version++
		}
	}
}

// Save writes the changelog atomically with owner-only permissions.
func (l *ChangeLog) Save() error {
	l.saveMu.Lock()
	defer l.saveMu.Unlock()
	l.mu.Lock()
	l.data.Version = 1
	data, err := json.Marshal(l.data)
	l.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0700); err != nil {
		return err
	}
	return writeConfigFile(l.path, data)
}
