package plugins

// These are standard mIRC IRC formatting controls. IRC clients that support
// colors render them; clients that do not will still receive the plain text.
const (
	ircReset  = "\x0f"
	ircBold   = "\x02"
	ircGreen  = "\x0303"
	ircRed    = "\x0304"
	ircCyan   = "\x0311"
	ircYellow = "\x0308"
)

func ircColor(color, text string) string {
	return color + text + ircReset
}
