package plugins

import (
	"strings"
	"unicode"
)

// readPuzzleClues reads operator-maintained prompt|answer[|alternate;answers]
// entries. A missing or malformed catalog is deliberately non-fatal: the
// built-in fallback clues keep the command usable after a fresh install.
func readPuzzleClues(path string) []puzzleClue {
	lines := readQuotes(path)
	clues := make([]puzzleClue, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 2 {
			continue
		}
		prompt := truncateRunes(cleanExternalText(strings.TrimSpace(parts[0])), 240)
		if prompt == "" {
			continue
		}
		answers := splitPuzzleAnswers(parts[1])
		if len(parts) == 3 {
			answers = append(answers, splitPuzzleAnswers(parts[2])...)
		}
		if len(answers) == 0 {
			continue
		}
		clues = append(clues, puzzleClue{Prompt: prompt, Answers: answers})
	}
	return clues
}

func splitPuzzleAnswers(raw string) []string {
	seen := make(map[string]struct{})
	answers := make([]string, 0, 2)
	for _, value := range strings.Split(raw, ";") {
		answer := truncateRunes(normalizePuzzleTextAnswer(value), 80)
		if answer == "" {
			continue
		}
		if _, exists := seen[answer]; exists {
			continue
		}
		seen[answer] = struct{}{}
		answers = append(answers, answer)
	}
	return answers
}

func normalizePuzzleTextAnswer(input string) string {
	cleaned := strings.ToLower(cleanExternalText(input))
	var out strings.Builder
	spacePending := false
	for _, r := range cleaned {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if spacePending && out.Len() > 0 {
				out.WriteByte(' ')
			}
			spacePending = false
			out.WriteRune(r)
			continue
		}
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			spacePending = true
		}
	}
	return strings.TrimSpace(out.String())
}

func puzzleTextAnswerMatches(input string, answers []string) bool {
	candidate := normalizePuzzleTextAnswer(input)
	if candidate == "" {
		return false
	}
	for _, answer := range answers {
		if candidate == normalizePuzzleTextAnswer(answer) {
			return true
		}
	}
	return false
}

func addPuzzleFallbacks(clues map[puzzleCategory][]puzzleClue) {
	fallbacks := map[puzzleCategory][]puzzleClue{
		puzzleTrivia: {
			{Prompt: "Which planet is known as the Red Planet?", Answers: []string{"mars"}},
			{Prompt: "What is the largest ocean on Earth?", Answers: []string{"pacific", "pacific ocean"}},
		},
		puzzleWord: {
			{Prompt: "Give a synonym for quick.", Answers: []string{"fast", "rapid", "swift"}},
			{Prompt: "What is the opposite of ancient?", Answers: []string{"modern", "new"}},
		},
		puzzleLogic: {
			{Prompt: "I have keys but no locks and space but no room. What am I?", Answers: []string{"keyboard", "a keyboard"}},
			{Prompt: "What comes next: 2, 4, 8, 16?", Answers: []string{"32", "thirty two", "thirty-two"}},
		},
		puzzleCrossword: {
			{Prompt: "Crossword-style clue: nocturnal bird of prey (3 letters).", Answers: []string{"owl"}},
			{Prompt: "Crossword-style clue: frozen water (3 letters).", Answers: []string{"ice"}},
		},
	}
	for category, fallback := range fallbacks {
		if len(clues[category]) == 0 {
			clues[category] = fallback
		}
	}
}
