package plugins

import (
	"fmt"
	"math/big"
	"math/rand"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

const (
	puzzleNumberCount = 6
	puzzleMinTarget   = 100
	puzzleMaxTarget   = 999
)

type puzzleCategory string

const (
	puzzleNumbers   puzzleCategory = "numbers"
	puzzleTrivia    puzzleCategory = "trivia"
	puzzleWord      puzzleCategory = "word"
	puzzleLogic     puzzleCategory = "logic"
	puzzleAnagram   puzzleCategory = "anagram"
	puzzleCrossword puzzleCategory = "crossword"
	puzzleRandom    puzzleCategory = "random"
)

var puzzleTextCategories = []puzzleCategory{puzzleTrivia, puzzleWord, puzzleLogic, puzzleAnagram, puzzleCrossword}

type Puzzle struct {
	mu        sync.Mutex
	games     map[string]*puzzleGame
	clues     map[puzzleCategory][]puzzleClue
	anagrams  []string
	timeout   time.Duration
	maxLength int
}

type puzzleGame struct {
	Category  puzzleCategory
	Target    int
	Numbers   []int
	Prompt    string
	Answers   []string
	TargetIRC string
	StartedAt time.Time
	Deadline  time.Time
	Timer     *time.Timer
	Best      *puzzleAttempt
}

type puzzleAttempt struct {
	Nick       string
	Expression string
	Value      *big.Rat
	Distance   *big.Rat
}

type puzzleClue struct {
	Prompt  string
	Answers []string
}

func (p *Puzzle) Name() string       { return "puzzle" }
func (p *Puzzle) Commands() []string { return []string{"puzzle", "puzzles"} }
func (p *Puzzle) Help() string {
	return "!puzzle <numbers|trivia|word|logic|anagram|crossword|random> — start a timed puzzle; !puzzle status|stop; alias: !puzzles"
}

func (p *Puzzle) Init(c bot.PluginConfig, _ *storage.DB) error {
	timeoutSeconds := c.Int("timeout_seconds", 45)
	if timeoutSeconds < 10 || timeoutSeconds > 300 {
		timeoutSeconds = 45
	}
	p.maxLength = c.Int("max_length", 360)
	if p.maxLength < 160 || p.maxLength > 600 {
		p.maxLength = 360
	}
	p.timeout = time.Duration(timeoutSeconds) * time.Second
	dataDir := c.String("data_dir", "data/puzzles")
	p.clues = map[puzzleCategory][]puzzleClue{
		puzzleTrivia:    readPuzzleClues(filepath.Join(dataDir, "trivia.txt")),
		puzzleWord:      readPuzzleClues(filepath.Join(dataDir, "word.txt")),
		puzzleLogic:     readPuzzleClues(filepath.Join(dataDir, "logic.txt")),
		puzzleCrossword: readPuzzleClues(filepath.Join(dataDir, "crossword.txt")),
	}
	p.anagrams = readScrambleWords(c.String("anagram_file", "data/scramble.txt"))
	if len(p.anagrams) == 0 {
		p.anagrams = append([]string(nil), scrambleFallbackWords...)
	}
	addPuzzleFallbacks(p.clues)
	p.mu.Lock()
	p.games = make(map[string]*puzzleGame)
	p.mu.Unlock()
	return nil
}

// Stop prevents an expired round from speaking after the plugin is disabled.
func (p *Puzzle) Stop(_ *bot.Bot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, game := range p.games {
		if game.Timer != nil {
			game.Timer.Stop()
		}
	}
	p.games = make(map[string]*puzzleGame)
}

func (p *Puzzle) Handle(b *bot.Bot, m bot.Message) bool {
	if !m.IsChannel || strings.TrimSpace(m.Nick) == "" {
		return false
	}
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if ok && isPuzzleCommand(cmd) {
		p.command(b, m, strings.TrimSpace(arg))
		return true
	}
	if ok {
		return false
	}
	return p.answer(b, m)
}

func (p *Puzzle) command(b *bot.Bot, m bot.Message, arg string) {
	key := puzzleStateKey(b.Config.NetworkName, m.Target)
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "status":
		p.status(b, m, key)
	case "stop", "cancel":
		p.cancel(b, m, key)
	default:
		category, ok := puzzleCategoryForArgument(arg)
		if !ok {
			b.Send(m.ReplyTarget(), "usage: !puzzle <numbers|trivia|word|logic|anagram|crossword|random>|status|stop")
			return
		}
		p.start(b, m, key, category)
	}
}

func (p *Puzzle) start(b *bot.Bot, m bot.Message, key string, category puzzleCategory) {
	p.mu.Lock()
	if current := p.games[key]; current != nil && time.Now().Before(current.Deadline) {
		remaining := int(time.Until(current.Deadline).Round(time.Second) / time.Second)
		if remaining < 1 {
			remaining = 1
		}
		response := fmt.Sprintf("⏱ > puzzle already active: %s, %ds remaining", current.Category, remaining)
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), truncateIRCMessage(response, p.maxLength))
		return
	}
	if current := p.games[key]; current != nil && current.Timer != nil {
		current.Timer.Stop()
	}
	game := p.newGame(category, m.ReplyTarget(), time.Now())
	p.games[key] = game
	game.Timer = time.AfterFunc(p.timeout, func() { p.expire(b, key, game) })
	p.mu.Unlock()

	response := formatPuzzleStart(game, int(p.timeout/time.Second))
	b.Send(m.ReplyTarget(), truncateIRCMessage(response, p.maxLength))
}

func (p *Puzzle) status(b *bot.Bot, m bot.Message, key string) {
	p.mu.Lock()
	game := p.games[key]
	if game == nil {
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), "⏱ > no puzzle is active; use !puzzle numbers to start one")
		return
	}
	if !time.Now().Before(game.Deadline) {
		response := p.finishLocked(key, game)
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), truncateIRCMessage(response, p.maxLength))
		return
	}
	remaining := int(time.Until(game.Deadline).Round(time.Second) / time.Second)
	if remaining < 1 {
		remaining = 1
	}
	response := formatPuzzleStatus(game, remaining)
	p.mu.Unlock()
	b.Send(m.ReplyTarget(), truncateIRCMessage(response, p.maxLength))
}

func (p *Puzzle) cancel(b *bot.Bot, m bot.Message, key string) {
	p.mu.Lock()
	game := p.games[key]
	if game == nil {
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), "⏱ > no puzzle is active")
		return
	}
	delete(p.games, key)
	if game.Timer != nil {
		game.Timer.Stop()
	}
	p.mu.Unlock()
	b.Send(m.ReplyTarget(), "⏱ > puzzle cancelled")
}

func (p *Puzzle) answer(b *bot.Bot, m bot.Message) bool {
	key := puzzleStateKey(b.Config.NetworkName, m.Target)
	expression := strings.TrimSpace(m.Text)

	p.mu.Lock()
	game := p.games[key]
	if game == nil {
		p.mu.Unlock()
		return false
	}
	if !time.Now().Before(game.Deadline) {
		response := p.finishLocked(key, game)
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), truncateIRCMessage(response, p.maxLength))
		return true
	}
	if isTextPuzzleCategory(game.Category) {
		if !puzzleTextAnswerMatches(expression, game.Answers) {
			p.mu.Unlock()
			return false
		}
		attempt := puzzleAttempt{Nick: cleanExternalText(m.Nick), Expression: normalizePuzzleTextAnswer(expression)}
		game.Best = &attempt
		response := fmt.Sprintf("⏱ > %s wins: %s (correct)!", attempt.Nick, attempt.Expression)
		delete(p.games, key)
		if game.Timer != nil {
			game.Timer.Stop()
		}
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), truncateIRCMessage(response, p.maxLength))
		return true
	}
	attempt, ok := evaluatePuzzleExpression(expression, game.Numbers, game.Target)
	if !ok {
		p.mu.Unlock()
		return false
	}
	attempt.Nick = cleanExternalText(m.Nick)
	better := game.Best == nil || attempt.Distance.Cmp(game.Best.Distance) < 0
	if better {
		game.Best = &attempt
	}
	if attempt.Distance.Sign() == 0 {
		response := fmt.Sprintf("⏱ > %s wins with %s = %s (exact)!", attempt.Nick, attempt.Expression, formatPuzzleRat(attempt.Value))
		delete(p.games, key)
		if game.Timer != nil {
			game.Timer.Stop()
		}
		p.mu.Unlock()
		b.Send(m.ReplyTarget(), truncateIRCMessage(response, p.maxLength))
		return true
	}
	var response string
	if better {
		response = fmt.Sprintf("⏱ > %s: new best — %s = %s (%s away)", attempt.Nick, attempt.Expression, formatPuzzleRat(attempt.Value), formatPuzzleRat(attempt.Distance))
	} else {
		response = fmt.Sprintf("⏱ > %s: nice try — %s = %s (%s away)", attempt.Nick, attempt.Expression, formatPuzzleRat(attempt.Value), formatPuzzleRat(attempt.Distance))
	}
	p.mu.Unlock()
	b.Send(m.ReplyTarget(), truncateIRCMessage(response, p.maxLength))
	return true
}

func (p *Puzzle) expire(b *bot.Bot, key string, game *puzzleGame) {
	p.mu.Lock()
	current := p.games[key]
	if current == nil || current != game {
		p.mu.Unlock()
		return
	}
	response := p.finishLocked(key, game)
	p.mu.Unlock()
	b.Send(game.TargetIRC, truncateIRCMessage(response, p.maxLength))
}

func (p *Puzzle) finishLocked(key string, game *puzzleGame) string {
	delete(p.games, key)
	if game.Timer != nil {
		game.Timer.Stop()
	}
	if game.Best == nil {
		if !isTextPuzzleCategory(game.Category) {
			return fmt.Sprintf("⏱ > Time's up. No valid answers for target %d.", game.Target)
		}
		return fmt.Sprintf("⏱ > Time's up. No correct answers for this %s puzzle.", game.Category)
	}
	if isTextPuzzleCategory(game.Category) {
		return fmt.Sprintf("⏱ > Time's up. The winner was %s with %s.", game.Best.Nick, game.Best.Expression)
	}
	return fmt.Sprintf("⏱ > Time's up. The winner was %s with %s = %s (%s away).", game.Best.Nick, game.Best.Expression, formatPuzzleRat(game.Best.Value), formatPuzzleRat(game.Best.Distance))
}

func isPuzzleCommand(command string) bool {
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "puzzle", "puzzles":
		return true
	default:
		return false
	}
}

func puzzleCategoryForArgument(arg string) (puzzleCategory, bool) {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "", "numbers", "number", "math", "start", "new":
		return puzzleNumbers, true
	case "trivia", "quiz":
		return puzzleTrivia, true
	case "word", "words", "vocabulary":
		return puzzleWord, true
	case "logic", "riddle", "riddles":
		return puzzleLogic, true
	case "anagram", "anagrams":
		return puzzleAnagram, true
	case "crossword", "clue", "clues":
		return puzzleCrossword, true
	case "random", "any":
		return puzzleRandom, true
	default:
		return "", false
	}
}

func puzzleStateKey(network, channel string) string {
	return strings.ToLower(strings.TrimSpace(network)) + "\x00" + strings.ToLower(strings.TrimSpace(channel))
}

func newPuzzleGame(target string, started time.Time, timeout time.Duration) *puzzleGame {
	return newNumbersPuzzleGame(target, started, timeout)
}

func newNumbersPuzzleGame(target string, started time.Time, timeout time.Duration) *puzzleGame {
	return &puzzleGame{
		Category:  puzzleNumbers,
		Target:    puzzleRandomTarget(),
		Numbers:   puzzleRandomNumbers(),
		TargetIRC: target,
		StartedAt: started,
		Deadline:  started.Add(timeout),
	}
}

func (p *Puzzle) newGame(category puzzleCategory, target string, started time.Time) *puzzleGame {
	if category == puzzleRandom {
		available := []puzzleCategory{puzzleNumbers}
		for _, candidate := range puzzleTextCategories {
			if candidate == puzzleAnagram {
				if len(p.anagrams) > 0 {
					available = append(available, candidate)
				}
				continue
			}
			if len(p.clues[candidate]) > 0 {
				available = append(available, candidate)
			}
		}
		category = available[rand.Intn(len(available))]
	}
	if category == puzzleNumbers {
		return newNumbersPuzzleGame(target, started, p.timeout)
	}
	game := &puzzleGame{Category: category, TargetIRC: target, StartedAt: started, Deadline: started.Add(p.timeout)}
	if category == puzzleAnagram {
		if len(p.anagrams) == 0 {
			p.anagrams = append([]string(nil), scrambleFallbackWords...)
		}
		word := p.anagrams[rand.Intn(len(p.anagrams))]
		game.Prompt = fmt.Sprintf("Anagram: unscramble %s", scrambleWord(word))
		game.Answers = []string{word}
		return game
	}
	clues := p.clues[category]
	if len(clues) == 0 {
		category = puzzleTrivia
		clues = p.clues[category]
		game.Category = category
	}
	clue := clues[rand.Intn(len(clues))]
	game.Prompt = fmt.Sprintf("%s: %s", puzzleCategoryLabel(category), clue.Prompt)
	game.Answers = append([]string(nil), clue.Answers...)
	return game
}

func puzzleRandomTarget() int {
	return puzzleMinTarget + rand.Intn(puzzleMaxTarget-puzzleMinTarget+1)
}

func puzzleRandomNumbers() []int {
	numbers := make([]int, 0, puzzleNumberCount)
	for i := 0; i < 4; i++ {
		numbers = append(numbers, 1+rand.Intn(10))
	}
	large := []int{25, 50, 75, 100}
	for i := 0; i < 2; i++ {
		numbers = append(numbers, large[rand.Intn(len(large))])
	}
	return numbers
}

func formatPuzzleNumbers(numbers []int) string {
	parts := make([]string, len(numbers))
	for i, number := range numbers {
		parts[i] = strconv.Itoa(number)
	}
	return strings.Join(parts, " ")
}

func formatPuzzleStart(game *puzzleGame, seconds int) string {
	if isTextPuzzleCategory(game.Category) {
		return fmt.Sprintf("⏱ > %s You have %d seconds.", game.Prompt, seconds)
	}
	return fmt.Sprintf("⏱ > Numbers: Get as close as possible to %d using the numbers %s (using only + - * / and parentheses). You have %d seconds.", game.Target, formatPuzzleNumbers(game.Numbers), seconds)
}

func formatPuzzleStatus(game *puzzleGame, remaining int) string {
	if isTextPuzzleCategory(game.Category) {
		return fmt.Sprintf("⏱ > %s (%ds remaining)", game.Prompt, remaining)
	}
	return fmt.Sprintf("⏱ > puzzle active: target %d, numbers %s, %ds remaining", game.Target, formatPuzzleNumbers(game.Numbers), remaining)
}

func puzzleCategoryLabel(category puzzleCategory) string {
	switch category {
	case puzzleTrivia:
		return "Trivia"
	case puzzleWord:
		return "Word"
	case puzzleLogic:
		return "Logic"
	case puzzleAnagram:
		return "Anagram"
	case puzzleCrossword:
		return "Crossword"
	default:
		return "Puzzle"
	}
}

func isTextPuzzleCategory(category puzzleCategory) bool {
	return category != "" && category != puzzleNumbers
}

func formatPuzzleRat(value *big.Rat) string {
	if value == nil {
		return "?"
	}
	if value.IsInt() {
		return value.Num().String()
	}
	formatted := value.FloatString(4)
	formatted = strings.TrimRight(strings.TrimRight(formatted, "0"), ".")
	return formatted
}

type puzzleTokenType uint8

const (
	puzzleTokenNumber puzzleTokenType = iota
	puzzleTokenPlus
	puzzleTokenMinus
	puzzleTokenMultiply
	puzzleTokenDivide
	puzzleTokenOpen
	puzzleTokenClose
)

type puzzleToken struct {
	typ   puzzleTokenType
	value int
}

type puzzleExpressionValue struct {
	value *big.Rat
	used  []int
}

type puzzleParser struct {
	tokens []puzzleToken
	pos    int
}

func evaluatePuzzleExpression(expression string, numbers []int, target int) (puzzleAttempt, bool) {
	tokens, ok := tokenizePuzzleExpression(expression)
	if !ok {
		return puzzleAttempt{}, false
	}
	parser := puzzleParser{tokens: tokens}
	value, ok := parser.parseExpression()
	if !ok || parser.pos != len(parser.tokens) || len(value.used) == 0 || !puzzleUsesAvailableNumbers(value.used, numbers) {
		return puzzleAttempt{}, false
	}
	distance := new(big.Rat).Sub(value.value, big.NewRat(int64(target), 1))
	distance.Abs(distance)
	return puzzleAttempt{Expression: normalizePuzzleExpression(expression), Value: value.value, Distance: distance}, true
}

func tokenizePuzzleExpression(expression string) ([]puzzleToken, bool) {
	if strings.TrimSpace(expression) == "" || utf8.RuneCountInString(expression) > 180 {
		return nil, false
	}
	tokens := make([]puzzleToken, 0, 16)
	for i := 0; i < len(expression); {
		r, size := utf8.DecodeRuneInString(expression[i:])
		if r == utf8.RuneError && size == 1 {
			return nil, false
		}
		if unicode.IsSpace(r) {
			i += size
			continue
		}
		if r >= '0' && r <= '9' {
			start := i
			for i < len(expression) && expression[i] >= '0' && expression[i] <= '9' {
				i++
			}
			number, err := strconv.Atoi(expression[start:i])
			if err != nil || number > 1000000 {
				return nil, false
			}
			tokens = append(tokens, puzzleToken{typ: puzzleTokenNumber, value: number})
			continue
		}
		typ, valid := puzzleOperatorToken(r)
		if !valid {
			return nil, false
		}
		tokens = append(tokens, puzzleToken{typ: typ})
		i += size
		if len(tokens) > 64 {
			return nil, false
		}
	}
	return tokens, len(tokens) > 0
}

func puzzleOperatorToken(r rune) (puzzleTokenType, bool) {
	switch r {
	case '+':
		return puzzleTokenPlus, true
	case '-':
		return puzzleTokenMinus, true
	case '*':
		return puzzleTokenMultiply, true
	case '/':
		return puzzleTokenDivide, true
	case '(':
		return puzzleTokenOpen, true
	case ')':
		return puzzleTokenClose, true
	default:
		return 0, false
	}
}

func (p *puzzleParser) parseExpression() (puzzleExpressionValue, bool) {
	left, ok := p.parseTerm()
	if !ok {
		return puzzleExpressionValue{}, false
	}
	for p.hasNext() && (p.peek().typ == puzzleTokenPlus || p.peek().typ == puzzleTokenMinus) {
		operator := p.next().typ
		right, rightOK := p.parseTerm()
		if !rightOK {
			return puzzleExpressionValue{}, false
		}
		left = combinePuzzleValues(left, right, operator)
	}
	return left, true
}

func (p *puzzleParser) parseTerm() (puzzleExpressionValue, bool) {
	left, ok := p.parseFactor()
	if !ok {
		return puzzleExpressionValue{}, false
	}
	for p.hasNext() && (p.peek().typ == puzzleTokenMultiply || p.peek().typ == puzzleTokenDivide) {
		operator := p.next().typ
		right, rightOK := p.parseFactor()
		if !rightOK || (operator == puzzleTokenDivide && right.value.Sign() == 0) {
			return puzzleExpressionValue{}, false
		}
		left = combinePuzzleValues(left, right, operator)
	}
	return left, true
}

func (p *puzzleParser) parseFactor() (puzzleExpressionValue, bool) {
	if !p.hasNext() {
		return puzzleExpressionValue{}, false
	}
	token := p.next()
	if token.typ == puzzleTokenNumber {
		return puzzleExpressionValue{value: big.NewRat(int64(token.value), 1), used: []int{token.value}}, true
	}
	if token.typ != puzzleTokenOpen {
		return puzzleExpressionValue{}, false
	}
	value, ok := p.parseExpression()
	if !ok || !p.hasNext() || p.next().typ != puzzleTokenClose {
		return puzzleExpressionValue{}, false
	}
	return value, true
}

func combinePuzzleValues(left, right puzzleExpressionValue, operator puzzleTokenType) puzzleExpressionValue {
	value := new(big.Rat)
	switch operator {
	case puzzleTokenPlus:
		value.Add(left.value, right.value)
	case puzzleTokenMinus:
		value.Sub(left.value, right.value)
	case puzzleTokenMultiply:
		value.Mul(left.value, right.value)
	case puzzleTokenDivide:
		value.Quo(left.value, right.value)
	}
	used := make([]int, 0, len(left.used)+len(right.used))
	used = append(used, left.used...)
	used = append(used, right.used...)
	return puzzleExpressionValue{value: value, used: used}
}

func (p *puzzleParser) hasNext() bool     { return p.pos < len(p.tokens) }
func (p *puzzleParser) peek() puzzleToken { return p.tokens[p.pos] }
func (p *puzzleParser) next() puzzleToken {
	token := p.tokens[p.pos]
	p.pos++
	return token
}

func puzzleUsesAvailableNumbers(used, available []int) bool {
	counts := make(map[int]int, len(available))
	for _, number := range available {
		counts[number]++
	}
	for _, number := range used {
		if counts[number] == 0 {
			return false
		}
		counts[number]--
	}
	return true
}

func normalizePuzzleExpression(expression string) string {
	return strings.Join(strings.Fields(expression), "")
}
