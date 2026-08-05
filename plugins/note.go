package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

const (
	noteBucket             = "notes"
	defaultMaxNotes        = 50
	defaultMaxNoteLength   = 400
	defaultNoteExpiryDays  = 180
	maxNoteNameLength      = 40
	maxConfiguredNoteCount = 500
)

type noteRecord struct {
	Name           string    `json:"name"`
	Text           string    `json:"text"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	LastAccessedAt time.Time `json:"last_accessed_at"`
}

type Note struct {
	db        *storage.DB
	mu        sync.Mutex
	maxNotes  int
	maxLength int
	expiry    time.Duration
}

func (p *Note) Name() string       { return "note" }
func (p *Note) Commands() []string { return []string{"note", "notes"} }
func (p *Note) Help() string {
	return "!note add <name> <text> — save a note; !note <name> — read it; !note list; !note delete <name>; !note clear"
}

func (p *Note) Init(c bot.PluginConfig, db *storage.DB) error {
	p.db = db
	p.maxNotes = c.Int("max_notes", defaultMaxNotes)
	if p.maxNotes < 1 || p.maxNotes > maxConfiguredNoteCount {
		p.maxNotes = defaultMaxNotes
	}
	p.maxLength = c.Int("max_note_length", defaultMaxNoteLength)
	if p.maxLength < 40 || p.maxLength > 2000 {
		p.maxLength = defaultMaxNoteLength
	}
	expiryDays := c.Int("expiry_days", defaultNoteExpiryDays)
	if expiryDays < 0 || expiryDays > 3650 {
		expiryDays = defaultNoteExpiryDays
	}
	if expiryDays > 0 {
		p.expiry = time.Duration(expiryDays) * 24 * time.Hour
	}
	p.cleanupExpired()
	return nil
}

func (p *Note) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "note" && cmd != "notes") {
		return false
	}
	identity := noteIdentity(b, m)
	if identity == "" || p.db == nil {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "note storage is unavailable"))
		return true
	}

	action, rest := splitNoteAction(arg)
	switch action {
	case "", "list":
		p.handleNoteList(b, m, identity)
	case "add", "save", "set":
		p.handleNoteAdd(b, m, identity, rest)
	case "get", "show":
		p.handleNoteGet(b, m, identity, rest)
	case "delete", "del", "remove":
		p.handleNoteDelete(b, m, identity, rest)
	case "clear", "all":
		p.handleNoteClear(b, m, identity, rest)
	default:
		p.handleNoteGet(b, m, identity, strings.TrimSpace(arg))
	}
	return true
}

func (p *Note) handleNoteList(b *bot.Bot, m bot.Message, identity string) {
	p.mu.Lock()
	notes, err := p.loadLocked(identity)
	p.mu.Unlock()
	if err != nil {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "couldn't read your notes"))
		return
	}
	if len(notes) == 0 {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "you have no saved notes"))
		return
	}
	names := make([]string, 0, len(notes))
	for name := range notes {
		names = append(names, name)
	}
	sort.Strings(names)
	response := "saved notes: " + strings.Join(names, ", ")
	for _, part := range splitIRCText(response, 350) {
		b.Send(m.ReplyTarget(), part)
	}
}

func (p *Note) handleNoteAdd(b *bot.Bot, m bot.Message, identity, arg string) {
	name, text, valid := parseNoteAdd(arg)
	if !valid {
		b.Send(m.ReplyTarget(), noteUsage())
		return
	}
	if !validNoteName(name) {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "note names must be one word, 40 characters or fewer, and contain no control characters"))
		return
	}
	text = cleanExternalText(text)
	if text == "" || len([]rune(text)) > p.maxLength {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, fmt.Sprintf("note text must be between 1 and %d characters", p.maxLength)))
		return
	}

	p.mu.Lock()
	notes, err := p.loadLocked(identity)
	if err == nil {
		now := time.Now()
		_, existed := notes[name]
		if !existed && len(notes) >= p.maxNotes {
			err = errNoteLimit
		} else {
			createdAt := now
			if previous, ok := notes[name]; ok {
				createdAt = previous.CreatedAt
			}
			notes[name] = noteRecord{Name: name, Text: text, CreatedAt: createdAt, UpdatedAt: now, LastAccessedAt: now}
			err = p.saveLocked(identity, notes)
		}
	}
	p.mu.Unlock()
	if errors.Is(err, errNoteLimit) {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, fmt.Sprintf("you already have the maximum of %d notes", p.maxNotes)))
		return
	}
	if err != nil {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "couldn't save that note"))
		return
	}
	b.Send(m.ReplyTarget(), ircColor(ircGreen, fmt.Sprintf("saved note %s", name)))
}

func (p *Note) handleNoteGet(b *bot.Bot, m bot.Message, identity, arg string) {
	name := strings.ToLower(strings.TrimSpace(arg))
	if !validNoteName(name) || len(strings.Fields(name)) != 1 {
		b.Send(m.ReplyTarget(), noteUsage())
		return
	}
	p.mu.Lock()
	notes, err := p.loadLocked(identity)
	note, found := notes[name]
	if err == nil && found {
		note.LastAccessedAt = time.Now()
		notes[name] = note
		err = p.saveLocked(identity, notes)
	}
	p.mu.Unlock()
	if err != nil {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "couldn't read your notes"))
		return
	}
	if !found {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, fmt.Sprintf("no note named %s", name)))
		return
	}
	for _, part := range splitIRCText(fmt.Sprintf("%s: %s", note.Name, note.Text), 350) {
		b.Send(m.ReplyTarget(), part)
	}
}

func (p *Note) handleNoteDelete(b *bot.Bot, m bot.Message, identity, arg string) {
	name := strings.ToLower(strings.TrimSpace(arg))
	if !validNoteName(name) || len(strings.Fields(name)) != 1 {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "usage: !note delete <name>"))
		return
	}
	p.mu.Lock()
	notes, err := p.loadLocked(identity)
	_, found := notes[name]
	if err == nil && found {
		delete(notes, name)
		err = p.saveLocked(identity, notes)
	}
	p.mu.Unlock()
	if err != nil {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "couldn't delete that note"))
		return
	}
	if !found {
		b.Send(m.ReplyTarget(), ircYellow+fmt.Sprintf("no note named %s", name)+ircReset)
		return
	}
	b.Send(m.ReplyTarget(), ircColor(ircGreen, fmt.Sprintf("deleted note %s", name)))
}

func (p *Note) handleNoteClear(b *bot.Bot, m bot.Message, identity, arg string) {
	if strings.TrimSpace(arg) != "" {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "usage: !note clear"))
		return
	}
	p.mu.Lock()
	notes, err := p.loadLocked(identity)
	if err == nil && len(notes) > 0 {
		err = p.db.Delete(noteBucket, noteStorageKey(identity))
	}
	p.mu.Unlock()
	if err != nil {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "couldn't clear your notes"))
		return
	}
	b.Send(m.ReplyTarget(), ircColor(ircGreen, fmt.Sprintf("cleared %d note(s)", len(notes))))
}

var errNoteLimit = errors.New("note limit reached")

func splitNoteAction(arg string) (string, string) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", ""
	}
	fields := strings.Fields(arg)
	return strings.ToLower(fields[0]), strings.TrimSpace(arg[len(fields[0]):])
}

func parseNoteAdd(arg string) (string, string, bool) {
	fields := strings.Fields(arg)
	if len(fields) < 2 {
		return "", "", false
	}
	name := strings.ToLower(fields[0])
	text := strings.TrimSpace(arg[len(fields[0]):])
	return name, text, text != ""
}

func validNoteName(name string) bool {
	return name != "" && len([]rune(name)) <= maxNoteNameLength && len(strings.Fields(name)) == 1 && !strings.ContainsRune(name, '\x00') && strings.IndexFunc(name, unicode.IsControl) < 0
}

func noteUsage() string {
	return ircColor(ircYellow, "usage: !note add <name> <text> | !note <name> | !note list | !note delete <name> | !note clear")
}

func noteIdentity(b *bot.Bot, m bot.Message) string {
	account := strings.TrimSpace(m.Account)
	if account != "" && account != "*" {
		return "account:" + strings.ToLower(account)
	}
	nick := strings.ToLower(strings.TrimSpace(m.Nick))
	if nick == "" {
		return ""
	}
	network := strings.ToLower(strings.TrimSpace(b.Config.NetworkName))
	return "nick:" + network + "\x00" + nick
}

func noteStorageKey(identity string) string {
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}

func (p *Note) loadLocked(identity string) (map[string]noteRecord, error) {
	if p.db == nil {
		return nil, errors.New("note storage is unavailable")
	}
	notes := make(map[string]noteRecord)
	raw, err := p.db.Get(noteBucket, noteStorageKey(identity))
	if errors.Is(err, storage.ErrNotFound) {
		return notes, nil
	}
	if err != nil {
		return nil, err
	}
	if err := storage.Decode(raw, &notes); err != nil {
		return nil, err
	}
	if p.expiry <= 0 {
		return notes, nil
	}
	changed := false
	now := time.Now()
	for name, note := range notes {
		if p.noteExpired(note, now) {
			delete(notes, name)
			changed = true
		}
	}
	if changed {
		if err := p.saveLocked(identity, notes); err != nil {
			return nil, err
		}
	}
	return notes, nil
}

func (p *Note) saveLocked(identity string, notes map[string]noteRecord) error {
	if len(notes) == 0 {
		return p.db.Delete(noteBucket, noteStorageKey(identity))
	}
	return p.db.Set(noteBucket, noteStorageKey(identity), notes)
}

func (p *Note) noteExpired(note noteRecord, now time.Time) bool {
	last := note.LastAccessedAt
	if last.IsZero() {
		last = note.UpdatedAt
	}
	if last.IsZero() {
		last = note.CreatedAt
	}
	return !last.IsZero() && now.Sub(last) >= p.expiry
}

func (p *Note) cleanupExpired() {
	if p.db == nil || p.expiry <= 0 {
		return
	}
	for _, key := range mustList(p.db, noteBucket) {
		raw, err := p.db.Get(noteBucket, key)
		if err != nil {
			continue
		}
		notes := make(map[string]noteRecord)
		if storage.Decode(raw, &notes) != nil {
			continue
		}
		changed := false
		now := time.Now()
		for name, note := range notes {
			if p.noteExpired(note, now) {
				delete(notes, name)
				changed = true
			}
		}
		if changed {
			if len(notes) == 0 {
				_ = p.db.Delete(noteBucket, key)
			} else {
				_ = p.db.Set(noteBucket, key, notes)
			}
		}
	}
}
