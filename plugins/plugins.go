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
		&Horoscope{},
		&Urban{},
		&XKCD{},
		&LastFM{},
		&Cats{},
		&EightBall{},
		&Cheer{},
		&Seen{},
		&Tell{},
		&Karma{},
		&Dice{},
		&Blackjack{},
		&Pool{},
		&Poll{},
		&Reminder{},
		&Quote{},
		&Choose{},
		&Timezone{},
		&ChannelStats{},
		&DuckHunt{},
		&Status{},
		&Define{},
		&Calculator{},
		&Reddit{},
		&Foods{},
		&Sports{},
		&Help{},
		&Alias{},
	}
}
