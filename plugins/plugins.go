package plugins

import "github.com/variablenix/GoBot/bot"

func All() []bot.Plugin {
	return []bot.Plugin{
		&Correction{},
		&Banter{},
		&URLTitle{},
		&Weather{},
		&News{},
		&Wikipedia{},
		&Seen{},
		&Tell{},
		&Karma{},
		&Dice{},
		&Blackjack{},
		&Help{},
	}
}
