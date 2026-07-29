package plugins

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type blackjackCard struct {
	rank string
	suit string
}

type blackjackGame struct {
	deck   []blackjackCard
	player []blackjackCard
	dealer []blackjackCard
}

type Blackjack struct {
	mu    sync.Mutex
	games map[string]*blackjackGame
}

func (p *Blackjack) Name() string { return "blackjack" }
func (p *Blackjack) Commands() []string {
	return []string{"21", "bj", "blackjack", "hit", "stand", "double"}
}
func (p *Blackjack) Help() string {
	return "!21 — start blackjack; use !hit, !stand, or !double during a game"
}
func (p *Blackjack) Init(_ bot.PluginConfig, _ *storage.DB) error {
	p.games = make(map[string]*blackjackGame)
	return nil
}

func (p *Blackjack) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || (cmd != "21" && cmd != "bj" && cmd != "blackjack" && cmd != "hit" && cmd != "stand" && cmd != "double") {
		return false
	}

	key := blackjackKey(m)
	p.mu.Lock()
	game := p.games[key]
	action := strings.ToLower(strings.TrimSpace(arg))
	if cmd == "hit" || cmd == "stand" || cmd == "double" {
		if action != "" {
			action = "invalid"
		} else {
			action = cmd
		}
	}
	var response string

	switch action {
	case "", "new", "start":
		if game != nil {
			response = blackjackStatus(game)
			break
		}
		game, err := newBlackjackGame()
		if err != nil {
			p.mu.Unlock()
			b.Send(m.ReplyTarget(), "blackjack is temporarily unavailable")
			return true
		}
		p.games[key] = game
		response = blackjackOpening(game)
	case "hit", "h":
		if game == nil {
			response = "no game in progress; use !21 to start"
			break
		}
		response = blackjackHit(game)
		if blackjackFinished(response) {
			delete(p.games, key)
		}
	case "stand", "s":
		if game == nil {
			response = "no game in progress; use !21 to start"
			break
		}
		response = blackjackStand(game)
		delete(p.games, key)
	case "double", "d":
		if game == nil {
			response = "no game in progress; use !21 to start"
			break
		}
		if len(game.player) != 2 {
			response = "double is only available on your first two cards"
			break
		}
		response = blackjackDouble(game)
		delete(p.games, key)
	default:
		response = "usage: !21 [hit|stand|double]"
	}
	p.mu.Unlock()
	b.Send(m.ReplyTarget(), response)
	return true
}

func blackjackKey(m bot.Message) string {
	return strings.ToLower(strings.TrimSpace(m.ReplyTarget())) + "\x00" + strings.ToLower(strings.TrimSpace(m.Nick))
}

func newBlackjackGame() (*blackjackGame, error) {
	game := &blackjackGame{deck: newBlackjackDeck()}
	if err := shuffleBlackjackDeck(game.deck); err != nil {
		return nil, err
	}
	for i := 0; i < 2; i++ {
		game.player = append(game.player, game.draw())
		game.dealer = append(game.dealer, game.draw())
	}
	return game, nil
}

func newBlackjackDeck() []blackjackCard {
	ranks := []string{"A", "2", "3", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K"}
	suits := []string{"clubs", "diamonds", "hearts", "spades"}
	deck := make([]blackjackCard, 0, 52)
	for _, suit := range suits {
		for _, rank := range ranks {
			deck = append(deck, blackjackCard{rank: rank, suit: suit})
		}
	}
	return deck
}

func shuffleBlackjackDeck(deck []blackjackCard) error {
	for i := len(deck) - 1; i > 0; i-- {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return err
		}
		j := int(n.Int64())
		deck[i], deck[j] = deck[j], deck[i]
	}
	return nil
}

func (g *blackjackGame) draw() blackjackCard {
	card := g.deck[len(g.deck)-1]
	g.deck = g.deck[:len(g.deck)-1]
	return card
}

func blackjackHandValue(hand []blackjackCard) (int, bool) {
	total, aces := 0, 0
	for _, card := range hand {
		switch card.rank {
		case "A":
			total += 11
			aces++
		case "K", "Q", "J":
			total += 10
		default:
			var value int
			_, _ = fmt.Sscanf(card.rank, "%d", &value)
			total += value
		}
	}
	for total > 21 && aces > 0 {
		total -= 10
		aces--
	}
	return total, aces > 0
}

func blackjackOpening(g *blackjackGame) string {
	playerValue, _ := blackjackHandValue(g.player)
	if playerValue == 21 {
		return blackjackStandResult(g, "blackjack")
	}
	return fmt.Sprintf("Your hand: %s = %d | Dealer shows: %s | !21 hit, stand, or double", formatBlackjackHand(g.player), playerValue, formatBlackjackCard(g.dealer[0]))
}

func blackjackStatus(g *blackjackGame) string {
	value, _ := blackjackHandValue(g.player)
	return fmt.Sprintf("Your hand: %s = %d | Dealer shows: %s | !21 hit, stand, or double", formatBlackjackHand(g.player), value, formatBlackjackCard(g.dealer[0]))
}

func blackjackHit(g *blackjackGame) string {
	g.player = append(g.player, g.draw())
	value, _ := blackjackHandValue(g.player)
	if value > 21 {
		return fmt.Sprintf("You bust: %s = %d. Dealer wins.", formatBlackjackHand(g.player), value)
	}
	if value == 21 {
		return blackjackStandResult(g, "21")
	}
	return fmt.Sprintf("Your hand: %s = %d | Dealer shows: %s", formatBlackjackHand(g.player), value, formatBlackjackCard(g.dealer[0]))
}

func blackjackDouble(g *blackjackGame) string {
	g.player = append(g.player, g.draw())
	value, _ := blackjackHandValue(g.player)
	if value > 21 {
		return fmt.Sprintf("Double down bust: %s = %d. Dealer wins.", formatBlackjackHand(g.player), value)
	}
	return blackjackStandResult(g, "double down")
}

func blackjackStand(g *blackjackGame) string {
	return blackjackStandResult(g, "stand")
}

func blackjackStandResult(g *blackjackGame, opening string) string {
	for value, _ := blackjackHandValue(g.dealer); value < 17; value, _ = blackjackHandValue(g.dealer) {
		g.dealer = append(g.dealer, g.draw())
	}
	playerValue, _ := blackjackHandValue(g.player)
	dealerValue, _ := blackjackHandValue(g.dealer)
	result := "You lose."
	if playerValue > 21 {
		result = "Dealer wins."
	} else if dealerValue > 21 || playerValue > dealerValue {
		result = "You win!"
	} else if playerValue == dealerValue {
		result = "Push — tie game."
	}
	return fmt.Sprintf("%s | You: %s = %d | Dealer: %s = %d | %s", opening, formatBlackjackHand(g.player), playerValue, formatBlackjackHand(g.dealer), dealerValue, result)
}

func blackjackFinished(response string) bool {
	return strings.Contains(response, "You bust:") || strings.Contains(response, "Dealer wins.") || strings.Contains(response, "You win!") || strings.Contains(response, "Push — tie game.") || strings.Contains(response, " | Dealer:")
}

func formatBlackjackHand(hand []blackjackCard) string {
	parts := make([]string, len(hand))
	for i, card := range hand {
		parts[i] = formatBlackjackCard(card)
	}
	return strings.Join(parts, ", ")
}

func formatBlackjackCard(card blackjackCard) string {
	symbols := map[string]string{"clubs": "C", "diamonds": "D", "hearts": "H", "spades": "S"}
	return card.rank + symbols[card.suit]
}
