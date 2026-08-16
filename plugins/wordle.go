package plugins

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type wordleState struct {
	Date     string         `json:"date"`
	Word     string         `json:"word"`
	Guesses  []string       `json:"guesses"`
	Slots    int            `json:"slots"`
	Wins     int            `json:"wins"`
	Losses   int            `json:"losses"`
	Hints    map[int]string `json:"hints,omitempty"`
	Finished bool           `json:"finished,omitempty"`
	Won      bool           `json:"won,omitempty"`
}

type Wordle struct {
	db         *storage.DB
	words      []string
	wordSet    map[string]struct{}
	maxGuesses int
	maxLength  int
	mu         sync.Mutex
}

func (p *Wordle) Name() string       { return "wordle" }
func (p *Wordle) Commands() []string { return []string{"wordle", "guess"} }
func (p *Wordle) Help() string {
	return "!wordle — show today's puzzle; !guess <word>; !wordle hint|give up|stats"
}
func (p *Wordle) Init(c bot.PluginConfig, db *storage.DB) error {
	p.db = db
	p.maxGuesses = c.Int("max_guesses", 6)
	if p.maxGuesses < 1 || p.maxGuesses > 10 {
		p.maxGuesses = 6
	}
	p.maxLength = c.Int("max_length", 400)
	if p.maxLength < 180 || p.maxLength > 450 {
		p.maxLength = 400
	}
	path := c.String("words_file", "data/wordle/words.txt")
	words, err := loadWordleWords(path)
	if err != nil {
		return err
	}
	p.words = words
	p.wordSet = make(map[string]struct{}, len(words))
	for _, word := range words {
		p.wordSet[word] = struct{}{}
	}
	return nil
}
func (p *Wordle) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "wordle" && cmd != "guess") {
		return false
	}
	if !m.IsChannel {
		b.Send(m.ReplyTarget(), "Wordle is available in channels only")
		return true
	}
	key := scopedKey(b.Config.NetworkName, m.Target, "wordle")
	p.mu.Lock()
	state := p.loadCurrentLocked(m.Target, key, time.Now().UTC())
	var response string
	switch cmd {
	case "guess":
		response = p.guessLocked(state, strings.TrimSpace(arg))
	case "wordle":
		parts := strings.Fields(arg)
		if len(parts) == 0 {
			response = formatWordleState(state, p.maxGuesses, p.maxLength)
		} else {
			switch strings.ToLower(parts[0]) {
			case "hint":
				response = p.hintLocked(state)
			case "give", "giveup":
				if len(parts) == 1 || strings.EqualFold(parts[0], "giveup") {
					response = p.giveUpLocked(state)
				} else {
					response = "usage: !wordle give up"
				}
			case "stats":
				response = fmt.Sprintf("Wordle stats in %s: %dW %dL", cleanExternalText(m.Target), state.Wins, state.Losses)
			default:
				response = "usage: !wordle [hint|give up|stats]"
			}
		}
	}
	p.persistLocked(key, state)
	p.mu.Unlock()
	b.Send(m.ReplyTarget(), truncateRunes(response, p.maxLength))
	return true
}

func loadWordleWords(path string) ([]string, error) {
	data, err := readBoundedRegularFile(path, maxWordleFileBytes)
	if err != nil {
		return nil, fmt.Errorf("load Wordle words: %w", err)
	}
	words := make([]string, 0, 2000)
	seen := make(map[string]struct{})
	for _, line := range strings.Split(string(data), "\n") {
		word := strings.TrimSpace(line)
		if len(word) != 5 || strings.ToLower(word) != word || strings.ContainsAny(word, " \t\r\n") || !isASCIIWord(word) {
			continue
		}
		if _, exists := seen[word]; !exists {
			seen[word] = struct{}{}
			words = append(words, word)
			if len(words) > 10000 {
				return nil, fmt.Errorf("Wordle words file contains more than 10000 words")
			}
		}
	}
	if len(words) < 2000 {
		return nil, fmt.Errorf("Wordle words file contains %d valid words; need at least 2000", len(words))
	}
	return words, nil
}

func isASCIIWord(word string) bool {
	for _, character := range word {
		if character < 'a' || character > 'z' {
			return false
		}
	}
	return true
}

func wordleAnswer(date, channel string, words []string) string {
	if len(words) == 0 {
		return ""
	}
	digest := sha256.Sum256([]byte(date + "\x00" + strings.ToLower(channel)))
	index := binary.BigEndian.Uint64(digest[:8]) % uint64(len(words))
	return words[index]
}

func (p *Wordle) loadCurrentLocked(channel, key string, now time.Time) *wordleState {
	state := &wordleState{Date: now.Format("2006-01-02"), Word: wordleAnswer(now.Format("2006-01-02"), channel, p.words), Hints: make(map[int]string)}
	if p.db != nil {
		if raw, err := p.db.Get("wordle", key); err == nil {
			var saved wordleState
			if storage.Decode(raw, &saved) == nil {
				if validWordleState(saved, state.Date, p.wordSet, p.maxGuesses) {
					state = &saved
					if state.Hints == nil {
						state.Hints = make(map[int]string)
					}
				} else {
					state.Wins, state.Losses = saved.Wins, saved.Losses
				}
			}
		}
	}
	return state
}

func validWordleState(state wordleState, date string, words map[string]struct{}, maxGuesses int) bool {
	if state.Date != date || state.Slots < 0 || state.Slots > maxGuesses || len(state.Guesses) > maxGuesses || state.Wins < 0 || state.Losses < 0 {
		return false
	}
	if _, ok := words[state.Word]; !ok {
		return false
	}
	for _, guess := range state.Guesses {
		if guess != "" {
			if _, ok := words[guess]; !ok {
				return false
			}
		} else if !state.Finished {
			return false
		}
	}
	for position, letter := range state.Hints {
		if position < 0 || position >= len(state.Word) || letter != string(state.Word[position]) {
			return false
		}
	}
	return len(state.Hints) == state.Slots && len(state.Guesses)+state.Slots <= maxGuesses
}
func (p *Wordle) persistLocked(key string, state *wordleState) {
	if p.db != nil {
		_ = p.db.Set("wordle", key, state)
	}
}
func (p *Wordle) guessLocked(state *wordleState, guess string) string {
	if state.Finished {
		return fmt.Sprintf("the puzzle is already complete; the word was: %s", strings.ToUpper(state.Word))
	}
	if len(guess) != 5 || strings.ToLower(guess) != guess {
		return "guess must be exactly five lowercase letters"
	}
	if _, ok := p.wordSet[guess]; !ok {
		return "❌ Not a valid word."
	}
	if len(state.Guesses)+state.Slots >= p.maxGuesses {
		state.Finished = true
		state.Losses++
		return fmt.Sprintf("❌ No more guesses! The word was: %s", strings.ToUpper(state.Word))
	}
	state.Guesses = append(state.Guesses, guess)
	if guess == state.Word {
		state.Finished = true
		state.Won = true
		state.Wins++
		return fmt.Sprintf("🟩 Solved in %d/%d! The word was: %s", len(state.Guesses), p.maxGuesses, strings.ToUpper(state.Word))
	}
	feedback := wordleFeedback(state.Word, guess)
	if len(state.Guesses)+state.Slots >= p.maxGuesses {
		state.Finished = true
		state.Losses++
		return fmt.Sprintf("❌ %s — No more guesses! The word was: %s", formatWordleGuess(len(state.Guesses), p.maxGuesses, feedback, state), strings.ToUpper(state.Word))
	}
	return formatWordleGuess(len(state.Guesses), p.maxGuesses, feedback, state)
}
func (p *Wordle) hintLocked(state *wordleState) string {
	if state.Finished {
		return fmt.Sprintf("the puzzle is already complete; the word was: %s", strings.ToUpper(state.Word))
	}
	if len(state.Guesses)+len(state.Hints) >= p.maxGuesses {
		return "no guess slots remain for a hint"
	}
	used := make(map[int]bool, len(state.Hints))
	for index := range state.Hints {
		used[index] = true
	}
	var choices []int
	for index := range state.Word {
		if !used[index] {
			choices = append(choices, index)
		}
	}
	if len(choices) == 0 {
		return "all answer positions are already revealed"
	}
	selected, err := secureRandomInt(int64(len(choices)))
	if err != nil {
		return "hint is temporarily unavailable"
	}
	position := choices[selected]
	state.Hints[position] = string(state.Word[position])
	state.Slots++
	return fmt.Sprintf("💡 Hint: position %d is '%s' (guess slot used: %d/%d)", position+1, strings.ToUpper(state.Hints[position]), len(state.Guesses)+state.Slots, p.maxGuesses)
}
func (p *Wordle) giveUpLocked(state *wordleState) string {
	if state.Finished {
		return fmt.Sprintf("the puzzle is already complete; the word was: %s", strings.ToUpper(state.Word))
	}
	if len(state.Guesses)+state.Slots >= p.maxGuesses {
		state.Finished = true
		state.Losses++
		return fmt.Sprintf("the puzzle is over; the word was: %s", strings.ToUpper(state.Word))
	}
	state.Losses++
	state.Finished = true
	state.Guesses = append(state.Guesses, "")
	return fmt.Sprintf("the word was: %s", strings.ToUpper(state.Word))
}

func wordleFeedback(answer, guess string) []string {
	if len(answer) != len(guess) {
		return nil
	}
	if len(answer) != len(guess) {
		return nil
	}
	feedback := make([]string, len(guess))
	remaining := make(map[byte]int)
	for i := range answer {
		if guess[i] == answer[i] {
			feedback[i] = "[" + strings.ToUpper(string(guess[i])) + "]"
		} else {
			remaining[answer[i]]++
		}
	}
	for i := range guess {
		if feedback[i] != "" {
			continue
		}
		if remaining[guess[i]] > 0 {
			feedback[i] = "(" + string(guess[i]) + ")"
			remaining[guess[i]]--
		} else {
			feedback[i] = "_"
		}
	}
	return feedback
}
func formatWordleGuess(number, max int, feedback []string, state *wordleState) string {
	letters := make([]string, 0, len(state.Guesses)+len(state.Hints))
	used := make(map[byte]bool)
	for _, prior := range state.Guesses {
		for i := range prior {
			if prior[i] != 0 && !used[prior[i]] {
				used[prior[i]] = true
				letters = append(letters, string(prior[i]))
			}
		}
	}
	return fmt.Sprintf("Guess %d/%d: %s — letters used: %s", number, max, strings.Join(feedback, " "), strings.Join(letters, ","))
}
func formatWordleState(state *wordleState, maxGuesses, maxLength int) string {
	if len(state.Guesses) == 0 && len(state.Hints) == 0 {
		return fmt.Sprintf("Wordle — 0/%d guesses used | puzzle ready", maxGuesses)
	}
	parts := make([]string, 0, len(state.Guesses)+len(state.Hints))
	for _, guess := range state.Guesses {
		if guess == "" {
			continue
		}
		parts = append(parts, strings.Join(wordleFeedback(state.Word, guess), " "))
	}
	positions := make([]int, 0, len(state.Hints))
	for position := range state.Hints {
		positions = append(positions, position)
	}
	sort.Ints(positions)
	for _, position := range positions {
		parts = append(parts, fmt.Sprintf("hint %d=%s", position+1, strings.ToUpper(state.Hints[position])))
	}
	result := fmt.Sprintf("Wordle — %d/%d guesses used | %s", len(state.Guesses)+state.Slots, maxGuesses, strings.Join(parts, " | "))
	return truncateRunes(result, maxLength)
}
