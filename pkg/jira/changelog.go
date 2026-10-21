package jira

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ChangeKind classifies one changelog entry.
type ChangeKind string

const (
	ChangeStatus        ChangeKind = "status"         // workflow status changed
	ChangeField         ChangeKind = "field"          // any other field changed
	ChangeComment       ChangeKind = "comment"        // comment added
	ChangeCommentEdited ChangeKind = "comment_edited" // existing comment edited
	ChangeAdded         ChangeKind = "added"          // ticket entered the instance's JQL filter
	ChangeRemoved       ChangeKind = "removed"        // ticket left the instance's JQL filter
)

// ChangeEntry is one line of the per-instance changelog.
type ChangeEntry struct {
	ID         string     `json:"id"` // unique within the instance; used to drop duplicates
	InstanceID string     `json:"instance_id"`
	BaseURL    string     `json:"base_url,omitempty"`
	IssueKey   string     `json:"issue_key"`
	Summary    string     `json:"summary,omitempty"`
	Kind       ChangeKind `json:"kind"`
	Field      string     `json:"field,omitempty"`
	From       string     `json:"from,omitempty"`
	To         string     `json:"to,omitempty"`
	Text       string     `json:"text,omitempty"` // comment excerpt
	CommentID  string     `json:"comment_id,omitempty"`
	Author     string     `json:"author,omitempty"`
	Own        bool       `json:"own,omitempty"`       // made by the API token's user
	Mentioned  bool       `json:"mentioned,omitempty"` // comment mentions the API token's user
	At         time.Time  `json:"at"`                  // when it happened in Jira
	Arrived    time.Time  `json:"arrived"`             // when this app recorded it; drives unread marks
}

// URL links to the ticket, scrolled to the comment for comment entries.
func (e ChangeEntry) URL() string {
	if e.BaseURL == "" || e.IssueKey == "" {
		return ""
	}
	u := fmt.Sprintf("%s/browse/%s", strings.TrimRight(e.BaseURL, "/"), e.IssueKey)
	if e.CommentID != "" {
		u += "?focusedCommentId=" + e.CommentID
	}
	return u
}

// userIdentity identifies the API token's user: accountId on Cloud, name/key on Data Center.
type userIdentity struct {
	AccountID string `json:"accountId"`
	Name      string `json:"name"`
	Key       string `json:"key"`
}

func (me userIdentity) matches(other userIdentity) bool {
	return (me.AccountID != "" && me.AccountID == other.AccountID) ||
		(me.Name != "" && me.Name == other.Name) ||
		(me.Key != "" && me.Key == other.Key)
}

// jiraUser is the author object of changelog histories and comments.
type jiraUser struct {
	userIdentity
	DisplayName string `json:"displayName"`
}

func (u jiraUser) display() string {
	return firstNonEmpty(u.DisplayName, u.Name, u.AccountID)
}

const (
	maxCommentExcerpt = 280
	maxFieldValue     = 80
)

// skippedChangelogFields are bookkeeping fields that would only add noise.
var skippedChangelogFields = map[string]bool{
	"rank": true, "timeestimate": true, "timeoriginalestimate": true, "timespent": true,
	"worklogid": true, "worklog id": true, "comment": true,
}

// longTextFields are shown as "edited" instead of their (long) old and new value.
var longTextFields = map[string]bool{"description": true, "environment": true}

type changelogHistory struct {
	ID      string   `json:"id"`
	Author  jiraUser `json:"author"`
	Created string   `json:"created"`
	Items   []struct {
		Field      string `json:"field"`
		FromString string `json:"fromString"`
		ToString   string `json:"toString"`
	} `json:"items"`
}

// historyEntries turns one changelog history into entries, one per changed field.
func historyEntries(h changelogHistory, base ChangeEntry, me userIdentity) []ChangeEntry {
	var out []ChangeEntry
	at := parseJiraTime(h.Created)
	for i, item := range h.Items {
		field := strings.TrimSpace(item.Field)
		lower := strings.ToLower(field)
		if field == "" || skippedChangelogFields[lower] {
			continue
		}
		e := base
		e.ID = fmt.Sprintf("h:%s:%d", h.ID, i)
		e.Kind = ChangeField
		e.Field = field
		e.From = strings.TrimSpace(item.FromString)
		e.To = strings.TrimSpace(item.ToString)
		e.Author = h.Author.display()
		e.Own = me.matches(h.Author.userIdentity)
		e.At = at
		switch {
		case lower == "status":
			e.Kind = ChangeStatus
			e.Field = "Status"
		case longTextFields[lower] || len(e.From) > maxFieldValue || len(e.To) > maxFieldValue:
			e.From, e.To = "", ""
		}
		out = append(out, e)
	}
	return out
}

type jiraComment struct {
	ID      string          `json:"id"`
	Author  jiraUser        `json:"author"`
	Body    json.RawMessage `json:"body"`
	Created string          `json:"created"`
	Updated string          `json:"updated"`
}

// commentEditGrace ignores the "updated" stamp Jira sets right after creating a comment.
const commentEditGrace = time.Minute

// commentEntry returns the entry for a comment that was added or edited after since.
func commentEntry(c jiraComment, base ChangeEntry, me userIdentity, since time.Time) (ChangeEntry, bool) {
	created := parseJiraTime(c.Created)
	updated := parseJiraTime(c.Updated)
	e := base
	e.CommentID = c.ID
	e.Author = c.Author.display()
	e.Own = me.matches(c.Author.userIdentity)
	text, mentions := commentText(c.Body)
	e.Text = excerpt(text, maxCommentExcerpt)
	for _, m := range mentions {
		if me.matches(m) {
			e.Mentioned = true
		}
	}
	switch {
	case created.After(since):
		e.ID = "c:" + c.ID
		e.Kind = ChangeComment
		e.At = created
	case updated.After(since) && updated.Sub(created) > commentEditGrace:
		e.ID = fmt.Sprintf("c:%s:%d", c.ID, updated.Unix())
		e.Kind = ChangeCommentEdited
		e.At = updated
	default:
		return ChangeEntry{}, false
	}
	return e, true
}

// commentText extracts plain text and mentioned users from a comment body, which is
// an Atlassian Document Format tree on REST v3 and wiki markup on REST v2.
func commentText(body json.RawMessage) (string, []userIdentity) {
	var wiki string
	if json.Unmarshal(body, &wiki) == nil {
		return wikiToText(wiki)
	}
	var doc adfNode
	if json.Unmarshal(body, &doc) != nil {
		return "", nil
	}
	var b strings.Builder
	var mentions []userIdentity
	doc.walk(&b, &mentions)
	return collapseSpace(b.String()), mentions
}

type adfNode struct {
	Type    string    `json:"type"`
	Text    string    `json:"text"`
	Content []adfNode `json:"content"`
	Attrs   struct {
		ID   string `json:"id"`
		Text string `json:"text"`
		URL  string `json:"url"`
	} `json:"attrs"`
}

func (n adfNode) walk(b *strings.Builder, mentions *[]userIdentity) {
	switch n.Type {
	case "text":
		b.WriteString(n.Text)
	case "mention":
		name := n.Attrs.Text
		if name != "" && !strings.HasPrefix(name, "@") {
			name = "@" + name
		}
		b.WriteString(name)
		*mentions = append(*mentions, userIdentity{AccountID: n.Attrs.ID})
	case "emoji":
		b.WriteString(n.Attrs.Text)
	case "inlineCard", "blockCard":
		b.WriteString(n.Attrs.URL)
	case "hardBreak":
		b.WriteByte(' ')
	case "mediaSingle", "mediaGroup":
		b.WriteString("[attachment]")
	}
	for _, c := range n.Content {
		c.walk(b, mentions)
	}
	switch n.Type {
	case "paragraph", "heading", "listItem", "codeBlock", "blockquote", "tableCell", "tableHeader", "panel":
		b.WriteByte(' ')
	}
}

var (
	wikiMention = regexp.MustCompile(`\[~(accountid:)?([^\]]+)\]`)
	wikiLink    = regexp.MustCompile(`\[([^|\]]+)\|[^\]]+\]`)
	wikiImage   = regexp.MustCompile(`![^!\s]+!`)
	wikiMacro   = regexp.MustCompile(`\{[a-zA-Z]+(:[^}]*)?\}`)
	wikiBold    = regexp.MustCompile(`\*([^*\n]+)\*`)
	wikiItalic  = regexp.MustCompile(`(^|[^\w])_([^_\n]+)_([^\w]|$)`)
	wikiHeading = regexp.MustCompile(`(?m)^h[1-6]\.\s*`)
)

// wikiToText strips the most common Jira wiki markup for a one-line excerpt.
func wikiToText(s string) (string, []userIdentity) {
	var mentions []userIdentity
	s = wikiMention.ReplaceAllStringFunc(s, func(m string) string {
		parts := wikiMention.FindStringSubmatch(m)
		if parts[1] != "" {
			mentions = append(mentions, userIdentity{AccountID: parts[2]})
			return "@user"
		}
		mentions = append(mentions, userIdentity{Name: parts[2], Key: parts[2]})
		return "@" + parts[2]
	})
	s = wikiLink.ReplaceAllString(s, "$1")
	s = wikiImage.ReplaceAllString(s, "[image]")
	s = wikiMacro.ReplaceAllString(s, " ")
	s = wikiHeading.ReplaceAllString(s, "")
	s = wikiBold.ReplaceAllString(s, "$1")
	s = wikiItalic.ReplaceAllString(s, "$1$2$3")
	return collapseSpace(s), mentions
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// excerpt shortens s to at most n runes, cutting at a word boundary when possible.
func excerpt(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	cut := string(r[:n])
	if i := strings.LastIndexByte(cut, ' '); i > n/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:") + "…"
}

var orderByKeyword = regexp.MustCompile(`(?i)^order\s+by\b`)

// stripOrderBy removes a trailing ORDER BY clause, ignoring "order by" inside quoted values.
func stripOrderBy(jql string) string {
	var quote rune
	escaped := false
	for i, r := range jql {
		switch {
		case escaped:
			escaped = false
		case r == '\\':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case (r == 'o' || r == 'O') && (i == 0 || !isJQLWordRune(jql[i-1])) && orderByKeyword.MatchString(jql[i:]):
			return strings.TrimSpace(jql[:i])
		}
	}
	return strings.TrimSpace(jql)
}

func isJQLWordRune(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// deltaJQL restricts an instance's filter to tickets updated within the last window.
// A relative date avoids converting to the Jira user's time zone.
func deltaJQL(jql string, window time.Duration) string {
	minutes := int(window/time.Minute) + 1
	cond := fmt.Sprintf(`updated >= "-%dm"`, minutes)
	filter := stripOrderBy(jql)
	if filter == "" {
		return cond + " ORDER BY updated ASC"
	}
	return fmt.Sprintf("(%s) AND %s ORDER BY updated ASC", filter, cond)
}
