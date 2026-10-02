package bot

import (
	"strings"
	"unicode/utf8"
)

// The IRC writer does not enforce the base protocol's 512-byte limit or
// remove line terminators. Enforce these once for every plugin's PRIVMSG.
// Split large results instead of silently dropping their trailing links.
func outgoingMessageParts(target, text string) []string {
	if target == "" || strings.ContainsAny(target, " \t\r\n\x00,") || strings.HasPrefix(target, ":") || !utf8.ValidString(target) {
		return nil
	}
	limit := 512 - len("PRIVMSG "+target+" :\r\n")
	if limit < 16 {
		return nil
	}
	text = strings.NewReplacer("\r", " ", "\n", " ", "\x00", "").Replace(strings.ToValidUTF8(text, ""))
	if len(text) <= limit {
		return []string{text}
	}
	var parts []string
	// Bound amplification even if an upstream returns an unexpectedly huge field.
	for len(text) > 0 && len(parts) < 10 {
		if len(text) <= limit {
			parts = append(parts, text)
			break
		}
		end := 0
		// Keep UTF-8 and mIRC color parameters intact. Reserve a reset byte so
		// a split formatted reply cannot bleed its style into client UI text.
		for end < len(text) {
			_, size := utf8.DecodeRuneInString(text[end:])
			if text[end] == '\x03' {
				size = outgoingColorSize(text[end:])
			}
			if end+size > limit-1 {
				break
			}
			end += size
		}
		part := text[:end]
		if strings.ContainsAny(part, "\x02\x03\x16\x1d\x1e\x1f") {
			part += "\x0f"
		}
		parts = append(parts, part)
		text = text[end:]
	}
	return parts
}

func outgoingColorSize(text string) int {
	i := 1
	for count := 0; i < len(text) && count < 2 && text[i] >= '0' && text[i] <= '9'; count++ {
		i++
	}
	if i+1 < len(text) && text[i] == ',' && text[i+1] >= '0' && text[i+1] <= '9' {
		i++
		for count := 0; i < len(text) && count < 2 && text[i] >= '0' && text[i] <= '9'; count++ {
			i++
		}
	}
	return i
}
