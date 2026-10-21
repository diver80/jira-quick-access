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
	mu     sync.Mutex
	saveMu sync.Mutex // keeps an older snapshot from replacing a newer one on disk
	path   string
	data   changeLogFile
}

type changeLogFile struct {
	Version   int                           `json:"version"`
	Instances map[string]*instanceChangeLog `json:"instances"`
}

type instanceChangeLog struct {
	Watermark time.Time     `json:"watermark"` // "since" of the next changes check
	ReadAt    time.Time     `json:"read_at"`   // entries up to here count as read
	Entries   []ChangeEntry `json:"entries"`   // newest first
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

// Merge adds entries of one instance, dropping those already stored, and returns the
// new ones. A non-zero watermark replaces the stored one. When markRead is set (the
// first backfill), everything up to now counts as read.
func (l *ChangeLog) Merge(instID string, entries []ChangeEntry, watermark time.Time, markRead bool, now time.Time) []ChangeEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	il := l.instanceLocked(instID)
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
		added = append(added, e)
	}
	il.Entries = append(il.Entries, added...)
	sortNewestFirst(il.Entries)
	il.Entries = pruneEntries(il.Entries, now)
	if !watermark.IsZero() {
		il.Watermark = watermark
	}
	if markRead {
		il.ReadAt = now
	}
	sortNewestFirst(added)
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

// ReadAt returns per instance up to when entries count as read.
func (l *ChangeLog) ReadAt() map[string]time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]time.Time, len(l.data.Instances))
	for id, il := range l.data.Instances {
		out[id] = il.ReadAt
	}
	return out
}

// Unread counts entries of the given instances (all when ids is empty) newer than
// their read mark. When skipOwn is set, the user's own changes are not counted.
func (l *ChangeLog) Unread(skipOwn bool, ids ...string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for id, il := range l.data.Instances {
		if len(ids) > 0 && !slices.Contains(ids, id) {
			continue
		}
		for _, e := range il.Entries {
			if !e.At.After(il.ReadAt) {
				break // newest first
			}
			if !(skipOwn && e.Own) {
				n++
			}
		}
	}
	return n
}

// MarkRead marks all entries of the given instances (all when ids is empty) up to t as read.
func (l *ChangeLog) MarkRead(t time.Time, ids ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for id, il := range l.data.Instances {
		if (len(ids) == 0 || slices.Contains(ids, id)) && t.After(il.ReadAt) {
			il.ReadAt = t
		}
	}
}

// Retain drops instances that are no longer configured.
func (l *ChangeLog) Retain(ids []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for id := range l.data.Instances {
		if !slices.Contains(ids, id) {
			delete(l.data.Instances, id)
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
