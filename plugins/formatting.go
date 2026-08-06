package plugins

import (
	"strings"
	"unicode/utf8"
)

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

// truncateIRCMessage limits visible text while preserving IRC formatting
// controls. It is used for messages assembled from both trusted labels and
// third-party response fields, where counting raw control bytes would make
// the configured limit misleading or cut a color sequence in half.
func truncateIRCMessage(text string, max int) string {
	if max <= 0 {
		return text
	}
	plain := strings.NewReplacer(
		ircReset, "",
		ircBold, "",
		ircGreen, "",
		ircRed, "",
		ircTan, "",
		ircCyan, "",
		ircYellow, "",
	).Replace(text)
	visible := utf8.RuneCountInString(plain)
	if visible <= max {
		return text
	}

	var out strings.Builder
	visible = 0
	for i := 0; i < len(text) && visible < max; {
		switch text[i] {
		case '\x02', '\x0f':
			out.WriteByte(text[i])
			i++
			continue
		case '\x03':
			start := i
			i++
			for i < len(text) && i-start <= 5 && ((text[i] >= '0' && text[i] <= '9') || text[i] == ',') {
				i++
			}
			out.WriteString(text[start:i])
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if r == utf8.RuneError && size == 1 {
			size = 1
		}
		out.WriteString(text[i : i+size])
		i += size
		visible++
	}
	out.WriteString(ircReset)
	return out.String()
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
