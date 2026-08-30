package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type triviaQuestion struct {
	Category string
	Text     string
	Options  []string
	Correct  int
}

type triviaGame struct {
	question triviaQuestion
	target   string
	timer    *time.Timer
}

type triviaScore struct {
	Nick    string `json:"nick"`
	Correct int    `json:"correct"`
}

type Trivia struct {
	db         *storage.DB
	answerTime time.Duration
	timeout    time.Duration
	cooldown   scopedCooldown
	maxLength  int
	fallbacks  []triviaQuestion
	mu         sync.Mutex
	active     map[string]*triviaGame
}

func (p *Trivia) Name() string       { return "trivia" }
func (p *Trivia) Commands() []string { return []string{"trivia", "t", "tq"} }
func (p *Trivia) Help() string {
	return "!trivia — start a question; !trivia stop|score|leaderboard; aliases: !t, !tq (the existing !q/!question ask commands are preserved)"
}
func (p *Trivia) Init(c bot.PluginConfig, db *storage.DB) error {
	p.db = db
	answerSeconds := c.Int("answer_seconds", 30)
	if answerSeconds < 5 || answerSeconds > 300 {
		answerSeconds = 30
	}
	timeoutSeconds := c.Int("timeout_seconds", 10)
	if timeoutSeconds < 1 || timeoutSeconds > 60 {
		timeoutSeconds = 10
	}
	p.answerTime = time.Duration(answerSeconds) * time.Second
	p.timeout = time.Duration(timeoutSeconds) * time.Second
	p.cooldown.configure(c.Int("cooldown_seconds", 5), 5)
	p.maxLength = c.Int("max_length", 400)
	if p.maxLength < 220 || p.maxLength > 450 {
		p.maxLength = 400
	}
	p.active = make(map[string]*triviaGame)
	p.fallbacks = append([]triviaQuestion(nil), triviaFallbacks...)
	if path := strings.TrimSpace(c.String("fallback_questions_file", "")); path != "" {
		loaded, err := loadTriviaFallbackFile(path)
		if err != nil {
			return err
		}
		p.fallbacks = append(p.fallbacks, loaded...)
	}
	return nil
}
func (p *Trivia) Stop(_ *bot.Bot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, game := range p.active {
		if game.timer != nil {
			game.timer.Stop()
		}
	}
	p.active = make(map[string]*triviaGame)
}
func (p *Trivia) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, isCommand := bot.IsCommand(m, b.Config.CommandPrefix)
	if !m.IsChannel {
		if isCommand && (cmd == "trivia" || cmd == "t" || cmd == "tq" || cmd == "answer") {
			b.Send(m.ReplyTarget(), "Trivia is available in channels only")
			return true
		}
		return false
	}
	key := scopedKey(b.Config.NetworkName, m.Target, "channel")
	if isCommand {
		if cmd != "trivia" && cmd != "t" && cmd != "question" && cmd != "tq" && cmd != "answer" {
			return false
		}
		if cmd == "answer" {
			return p.answer(b, m, strings.TrimSpace(arg), key)
		}
		parts := strings.Fields(arg)
		if len(parts) > 0 && strings.EqualFold(parts[0], "stop") {
			if !b.IsOwner(m) {
				b.Send(m.ReplyTarget(), "only a channel owner can stop trivia")
				return true
			}
			p.stopGame(key)
			b.Send(m.ReplyTarget(), "trivia stopped")
			return true
		}
		if len(parts) > 0 && (strings.EqualFold(parts[0], "score") || strings.EqualFold(parts[0], "scores")) {
			p.sendScore(b, m)
			return true
		}
		if len(parts) > 0 && strings.EqualFold(parts[0], "leaderboard") {
			p.sendLeaderboard(b, m)
			return true
		}
		if !p.cooldown.allow(key) {
			return true
		}
		p.start(b, m, key)
		return true
	}
	if m.IsChannel && len(strings.TrimSpace(m.Text)) == 1 && strings.ContainsAny(strings.ToUpper(m.Text), "ABCD") {
		return p.answer(b, m, m.Text, key)
	}
	return false
}

func (p *Trivia) start(b *bot.Bot, m bot.Message, key string) {
	p.mu.Lock()
	if _, exists := p.active[key]; exists {
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), "A question is already active — answer with A, B, C, or D!")
		return
	}
	p.mu.Unlock()
	question := p.fetchQuestion()
	response := formatTriviaQuestion(question, p.maxLength)
	timer := time.AfterFunc(p.answerTime, func() { p.expire(b, key) })
	p.mu.Lock()
	if _, exists := p.active[key]; exists {
		timer.Stop()
		p.mu.Unlock()
		return
	}
	p.active[key] = &triviaGame{question: question, target: m.ReplyTarget(), timer: timer}
	p.mu.Unlock()
	b.Send(m.ReplyTarget(), response)
}

func (p *Trivia) answer(b *bot.Bot, m bot.Message, raw, key string) bool {
	letter := strings.ToUpper(strings.TrimSpace(raw))
	if len(letter) != 1 || !strings.Contains("ABCD", letter) {
		return false
	}
	p.mu.Lock()
	game, exists := p.active[key]
	if !exists {
		p.mu.Unlock()
		return false
	}
	if game.question.Correct != int(letter[0]-'A') {
		p.mu.Unlock()
		return true
	}
	delete(p.active, key)
	if game.timer != nil {
		game.timer.Stop()
	}
	question := game.question
	p.mu.Unlock()
	p.incrementScore(b.Config.NetworkName, m.Target, m)
	answer := question.Options[question.Correct]
	b.Send(m.ReplyTarget(), fmt.Sprintf("✅ %s got it! The answer was %s) %s (+1 point)", truncateRunes(cleanExternalText(m.Nick), 48), letter, cleanExternalText(answer)))
	return true
}

func (p *Trivia) expire(b *bot.Bot, key string) {
	p.mu.Lock()
	game, exists := p.active[key]
	if exists {
		delete(p.active, key)
	}
	p.mu.Unlock()
	if !exists {
		return
	}
	answer := game.question.Options[game.question.Correct]
	b.Send(game.target, fmt.Sprintf("⏱️ Time's up! The answer was %c) %s", triviaOptionLabel(game.question.Correct), cleanExternalText(answer)))
}

func (p *Trivia) stopGame(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if game := p.active[key]; game != nil && game.timer != nil {
		game.timer.Stop()
	}
	delete(p.active, key)
}

func (p *Trivia) fetchQuestion() triviaQuestion {
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://opentdb.com/api.php?amount=1&type=multiple", nil)
	if err == nil {
		req.Header.Set("User-Agent", "GoBot/1.0 (IRC bot)")
		if res, requestErr := apiHTTPClient.Do(req); requestErr == nil {
			defer res.Body.Close()
			if res.StatusCode == http.StatusOK {
				var payload struct {
					ResponseCode int `json:"response_code"`
					Results      []struct {
						Category string   `json:"category"`
						Question string   `json:"question"`
						Correct  string   `json:"correct_answer"`
						Wrong    []string `json:"incorrect_answers"`
					} `json:"results"`
				}
				if json.NewDecoder(io.LimitReader(res.Body, 128<<10)).Decode(&payload) == nil && len(payload.Results) > 0 {
					result := payload.Results[0]
					options := append([]string{html.UnescapeString(result.Correct)}, result.Wrong...)
					if shuffleStrings(options) == nil && validTriviaQuestion(result.Category, result.Question, options) {
						for i, option := range options {
							if option == html.UnescapeString(result.Correct) {
								return triviaQuestion{Category: cleanExternalText(html.UnescapeString(result.Category)), Text: cleanExternalText(html.UnescapeString(result.Question)), Options: cleanTriviaOptions(options), Correct: i}
							}
						}
					}
				}
			}
		}
	}
	return fallbackTriviaQuestion(p.fallbacks)
}

func shuffleStrings(values []string) error {
	for i := len(values) - 1; i > 0; i-- {
		index, err := secureRandomInt(int64(i + 1))
		if err != nil {
			return err
		}
		values[i], values[index] = values[index], values[i]
	}
	return nil
}

func cleanTriviaOptions(values []string) []string {
	options := make([]string, len(values))
	for i, value := range values {
		options[i] = truncateRunes(cleanExternalText(html.UnescapeString(value)), 35)
	}
	return options
}

func validTriviaQuestion(category, question string, options []string) bool {
	if strings.TrimSpace(category) == "" || strings.TrimSpace(question) == "" || len(options) != 4 {
		return false
	}
	seen := make(map[string]struct{}, len(options))
	for _, option := range options {
		cleaned := cleanExternalText(html.UnescapeString(option))
		if cleaned == "" {
			return false
		}
		if _, exists := seen[strings.ToLower(cleaned)]; exists {
			return false
		}
		seen[strings.ToLower(cleaned)] = struct{}{}
	}
	return true
}

func formatTriviaQuestion(question triviaQuestion, maxLength int) string {
	options := make([]string, len(question.Options))
	for i, option := range question.Options {
		options[i] = fmt.Sprintf("%c) %s", triviaOptionLabel(i), cleanExternalText(option))
	}
	suffix := " | " + strings.Join(options, "  ")
	prefix := "❓ [" + cleanExternalText(question.Category) + "] "
	available := maxLength - len([]rune(prefix)) - len([]rune(suffix))
	text := truncateRunes(strings.TrimSuffix(strings.TrimSpace(question.Text), "?"), available)
	return truncateRunes(prefix+text+"?"+suffix, maxLength)
}

func triviaOptionLabel(index int) rune {
	labels := [...]rune{'A', 'B', 'C', 'D'}
	if index < 0 || index >= len(labels) {
		return '?'
	}
	return labels[index]
}

var triviaFallbacks = []triviaQuestion{
	{Category: "Science", Text: "What planet is known as the Red Planet", Options: []string{"Mars", "Venus", "Jupiter", "Mercury"}, Correct: 0},
	{Category: "Geography", Text: "What is the capital of Japan", Options: []string{"Tokyo", "Kyoto", "Osaka", "Sapporo"}, Correct: 0},
	{Category: "History", Text: "Who was the first person to walk on the Moon", Options: []string{"Neil Armstrong", "Yuri Gagarin", "Buzz Aldrin", "John Glenn"}, Correct: 0},
	{Category: "Computing", Text: "What does CPU stand for", Options: []string{"Central Processing Unit", "Computer Personal Unit", "Core Program Utility", "Central Program User"}, Correct: 0},
	{Category: "Nature", Text: "How many legs does a spider have", Options: []string{"Eight", "Six", "Ten", "Twelve"}, Correct: 0},
	{Category: "Art", Text: "Who painted the Mona Lisa", Options: []string{"Leonardo da Vinci", "Vincent van Gogh", "Claude Monet", "Pablo Picasso"}, Correct: 0},
	{Category: "Literature", Text: "Who wrote Pride and Prejudice", Options: []string{"Jane Austen", "Mary Shelley", "George Eliot", "Emily Bronte"}, Correct: 0},
	{Category: "Music", Text: "How many strings does a standard violin have", Options: []string{"Four", "Three", "Five", "Six"}, Correct: 0},
	{Category: "Food", Text: "What is the main ingredient in hummus", Options: []string{"Chickpeas", "Lentils", "Potatoes", "Peanuts"}, Correct: 0},
	{Category: "Space", Text: "What is the largest planet in our solar system", Options: []string{"Jupiter", "Saturn", "Earth", "Neptune"}, Correct: 0},
}

func fallbackTriviaQuestion(bank []triviaQuestion) triviaQuestion {
	if len(bank) == 0 {
		bank = triviaFallbacks
	}
	index, err := secureRandomInt(int64(len(bank)))
	if err != nil {
		index = 0
	}
	question := bank[index]
	question.Options = append([]string(nil), question.Options...)
	correct := question.Options[question.Correct]
	if shuffleStrings(question.Options) == nil {
		for i, option := range question.Options {
			if option == correct {
				question.Correct = i
				break
			}
		}
	}
	return question
}

func loadTriviaFallbackFile(path string) ([]triviaQuestion, error) {
	data, err := readBoundedRegularFile(path, maxTriviaFileBytes)
	if err != nil {
		return nil, fmt.Errorf("load trivia fallback questions: %w", err)
	}
	var raw []struct {
		Category         string   `json:"category"`
		Question         string   `json:"question"`
		CorrectAnswer    string   `json:"correct_answer"`
		IncorrectAnswers []string `json:"incorrect_answers"`
		Options          []string `json:"options"`
		Correct          int      `json:"correct"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse trivia fallback questions: %w", err)
	}
	questions := make([]triviaQuestion, 0, len(raw))
	for _, item := range raw {
		if len(questions) >= 1000 {
			break
		}
		options := append([]string(nil), item.Options...)
		correct := item.Correct
		if len(options) == 0 && item.CorrectAnswer != "" {
			options = append([]string{item.CorrectAnswer}, item.IncorrectAnswers...)
			correct = 0
		}
		if correct < 0 || correct >= len(options) || !validTriviaQuestion(item.Category, item.Question, options) {
			continue
		}
		questions = append(questions, triviaQuestion{Category: cleanExternalText(item.Category), Text: cleanExternalText(item.Question), Options: cleanTriviaOptions(options), Correct: correct})
	}
	return questions, nil
}

func (p *Trivia) incrementScore(network, channel string, m bot.Message) {
	if p.db == nil {
		return
	}
	key := scopedKey(network, channel, pluginIdentity(m))
	record := triviaScore{Nick: cleanExternalText(m.Nick)}
	if raw, err := p.db.Get("trivia", key); err == nil {
		if storage.Decode(raw, &record) != nil {
			record = triviaScore{}
		}
	}
	record.Correct = maxNonNegative(record.Correct)
	record.Nick = cleanExternalText(m.Nick)
	record.Correct++
	_ = p.db.Set("trivia", key, record)
}
func (p *Trivia) sendScore(b *bot.Bot, m bot.Message) {
	if p.db == nil {
		b.Send(m.ReplyTarget(), "trivia scores are unavailable")
		return
	}
	key := scopedKey(b.Config.NetworkName, m.Target, pluginIdentity(m))
	var score triviaScore
	if raw, err := p.db.Get("trivia", key); err == nil {
		if storage.Decode(raw, &score) != nil {
			score = triviaScore{}
		}
	}
	score.Correct = maxNonNegative(score.Correct)
	b.Send(m.ReplyTarget(), fmt.Sprintf("📊 Your trivia score in %s: %d", cleanExternalText(m.Target), score.Correct))
}
func (p *Trivia) sendLeaderboard(b *bot.Bot, m bot.Message) {
	if p.db == nil {
		b.Send(m.ReplyTarget(), "🏆 No trivia scores are available")
		return
	}
	prefix := scopedKey(b.Config.NetworkName, m.Target, "")
	keys, _ := p.db.List("trivia")
	leaders := make([]triviaScore, 0, len(keys))
	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		raw, err := p.db.Get("trivia", key)
		if err != nil {
			continue
		}
		var score triviaScore
		if storage.Decode(raw, &score) == nil && score.Correct >= 0 {
			score.Correct = maxNonNegative(score.Correct)
			leaders = append(leaders, score)
		}
	}
	sort.SliceStable(leaders, func(i, j int) bool {
		if leaders[i].Correct != leaders[j].Correct {
			return leaders[i].Correct > leaders[j].Correct
		}
		return strings.ToLower(leaders[i].Nick) < strings.ToLower(leaders[j].Nick)
	})
	if len(leaders) > 5 {
		leaders = leaders[:5]
	}
	parts := make([]string, len(leaders))
	for i, score := range leaders {
		parts[i] = fmt.Sprintf("%s %d", cleanExternalText(score.Nick), score.Correct)
	}
	if len(parts) == 0 {
		b.Send(m.ReplyTarget(), "🏆 No trivia scores are available")
		return
	}
	b.Send(m.ReplyTarget(), truncateRunes("🏆 Trivia leaders in "+cleanExternalText(m.Target)+": "+strings.Join(parts, ", "), p.maxLength))
}
