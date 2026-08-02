package plugins

import "strings"

// These are standard mIRC IRC formatting controls. IRC clients that support
// colors render them; clients that do not will still receive the plain text.
const (
	ircReset  = "\x0f"
	ircBold   = "\x02"
	ircGreen  = "\x0303"
	ircRed    = "\x0304"
	ircTan    = "\x0307" // mIRC orange, a practical tan approximation
	ircCyan   = "\x0311"
	ircYellow = "\x0308"
)

func ircColor(color, text string) string {
	return color + text + ircReset
}

// cleanExternalText removes IRC control characters from third-party content.
// This prevents API responses from changing client formatting or injecting
// line breaks into a channel response.
func cleanExternalText(text string) string {
	text = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, text)
	return strings.Join(strings.Fields(text), " ")
}
