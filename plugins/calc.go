package plugins

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type Calculator struct{}

func (p *Calculator) Name() string       { return "calc" }
func (p *Calculator) Commands() []string { return []string{"calc", "math", "convert"} }
func (p *Calculator) Help() string {
	return "!calc <expression> or !convert <number> <unit> to <unit> — safe local math and unit conversion"
}
func (p *Calculator) Init(_ bot.PluginConfig, _ *storage.DB) error { return nil }

func (p *Calculator) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || !isCalculatorCommand(cmd) {
		return false
	}
	var response string
	var err error
	if strings.EqualFold(cmd, "convert") {
		response, err = convertExpression(arg)
	} else {
		response, err = calculateExpression(arg)
	}
	if err != nil {
		b.Send(m.ReplyTarget(), ircColor(ircYellow, "usage: !calc 2*(3+4) or !convert 10 km to mi"))
		return true
	}
	b.Send(m.ReplyTarget(), response)
	return true
}

func isCalculatorCommand(command string) bool {
	switch strings.ToLower(command) {
	case "calc", "math", "convert":
		return true
	default:
		return false
	}
}

func calculateExpression(expression string) (string, error) {
	expression = strings.TrimSpace(expression)
	if expression == "" || len([]rune(expression)) > 80 {
		return "", fmt.Errorf("invalid expression")
	}
	input := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, expression)
	parser := arithmeticParser{input: input}
	value, err := parser.parseExpression()
	if err != nil || parser.position != len(parser.input) || !finiteNumber(value) {
		return "", fmt.Errorf("invalid expression")
	}
	return fmt.Sprintf("calc: %s = %s", expression, formatNumber(value)), nil
}

type arithmeticParser struct {
	input    string
	position int
}

func (p *arithmeticParser) parseExpression() (float64, error) {
	value, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for p.position < len(p.input) {
		switch p.input[p.position] {
		case '+':
			p.position++
			right, err := p.parseTerm()
			if err != nil {
				return 0, err
			}
			value += right
		case '-':
			p.position++
			right, err := p.parseTerm()
			if err != nil {
				return 0, err
			}
			value -= right
		default:
			return value, nil
		}
	}
	return value, nil
}

func (p *arithmeticParser) parseTerm() (float64, error) {
	value, err := p.parseFactor()
	if err != nil {
		return 0, err
	}
	for p.position < len(p.input) {
		switch p.input[p.position] {
		case '*', '/':
			operator := p.input[p.position]
			p.position++
			right, err := p.parseFactor()
			if err != nil || (operator == '/' && right == 0) {
				return 0, fmt.Errorf("invalid term")
			}
			if operator == '*' {
				value *= right
			} else {
				value /= right
			}
		default:
			return value, nil
		}
	}
	return value, nil
}

func (p *arithmeticParser) parseFactor() (float64, error) {
	if p.position >= len(p.input) {
		return 0, fmt.Errorf("missing factor")
	}
	if p.input[p.position] == '+' || p.input[p.position] == '-' {
		negative := p.input[p.position] == '-'
		p.position++
		value, err := p.parseFactor()
		if negative {
			value = -value
		}
		return value, err
	}
	if p.input[p.position] == '(' {
		p.position++
		value, err := p.parseExpression()
		if err != nil || p.position >= len(p.input) || p.input[p.position] != ')' {
			return 0, fmt.Errorf("missing parenthesis")
		}
		p.position++
		return value, nil
	}
	start := p.position
	digits := false
	decimal := false
	for p.position < len(p.input) {
		ch := p.input[p.position]
		if ch >= '0' && ch <= '9' {
			digits = true
			p.position++
			continue
		}
		if ch == '.' && !decimal {
			decimal = true
			p.position++
			continue
		}
		break
	}
	if !digits {
		return 0, fmt.Errorf("missing number")
	}
	return strconv.ParseFloat(p.input[start:p.position], 64)
}

func convertExpression(expression string) (string, error) {
	parts := strings.Fields(strings.ToLower(strings.TrimSpace(expression)))
	if len(parts) != 4 || parts[2] != "to" {
		return "", fmt.Errorf("invalid conversion")
	}
	value, err := strconv.ParseFloat(parts[0], 64)
	if err != nil || !finiteNumber(value) {
		return "", fmt.Errorf("invalid number")
	}
	converted, ok := convertValue(value, parts[1], parts[3])
	if !ok {
		return "", fmt.Errorf("unsupported units")
	}
	return fmt.Sprintf("convert: %s %s = %s %s", parts[0], parts[1], formatNumber(converted), parts[3]), nil
}

func convertValue(value float64, from, to string) (float64, bool) {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == to {
		return value, true
	}
	if f, ok := temperatureToCelsius(value, from); ok {
		if result, ok := celsiusFrom(f, to); ok {
			return result, true
		}
	}
	groups := []map[string]float64{
		{"mm": 0.001, "cm": 0.01, "m": 1, "km": 1000, "in": 0.0254, "ft": 0.3048, "yd": 0.9144, "mi": 1609.344},
		{"mg": 0.001, "g": 1, "kg": 1000, "oz": 28.349523125, "lb": 453.59237},
		{"ml": 0.001, "l": 1, "liter": 1, "liters": 1, "gal": 3.785411784},
	}
	for _, group := range groups {
		if fromFactor, ok := group[from]; ok {
			if toFactor, ok := group[to]; ok {
				return value * fromFactor / toFactor, true
			}
		}
	}
	return 0, false
}

func temperatureToCelsius(value float64, unit string) (float64, bool) {
	switch unit {
	case "c", "°c", "celsius":
		return value, true
	case "f", "°f", "fahrenheit":
		return (value - 32) * 5 / 9, true
	case "k", "°k", "kelvin":
		return value - 273.15, true
	default:
		return 0, false
	}
}

func celsiusFrom(value float64, unit string) (float64, bool) {
	switch unit {
	case "c", "°c", "celsius":
		return value, true
	case "f", "°f", "fahrenheit":
		return value*9/5 + 32, true
	case "k", "°k", "kelvin":
		return value + 273.15, true
	default:
		return 0, false
	}
}

func finiteNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && math.Abs(value) <= 1e100
}

func formatNumber(value float64) string {
	value = math.Round(value*1e6) / 1e6
	return strconv.FormatFloat(value, 'f', -1, 64)
}
