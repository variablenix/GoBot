package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

type Weather struct {
	cfg   bot.PluginConfig
	db    *storage.DB
	mu    sync.Mutex
	cache map[string]weatherCache
}

var apiHTTPClient = &http.Client{Timeout: 10 * time.Second}

type weatherCache struct {
	body     string
	location string
	at       time.Time
}

const weatherDefaultsBucket = "weather_defaults"

func (p *Weather) Name() string {
	return "weather"
}

func (p *Weather) Commands() []string {
	return []string{"weather", "wx", "forecast", "temp"}
}
func (p *Weather) Help() string {
	return "!weather [city] — current weather; !weather set <city> saves your default; !weather clear removes it (aliases: !wx, !forecast, !temp; no API key required)"
}
func (p *Weather) Init(c bot.PluginConfig, db *storage.DB) error {
	p.cfg = c
	p.db = db
	p.cache = map[string]weatherCache{}
	return nil
}
func (p *Weather) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || !isWeatherCommand(cmd) {
		return false
	}
	request, valid := parseWeatherRequest(arg)
	if !valid {
		b.Send(m.ReplyTarget(), weatherUsage())
		return true
	}
	identity := weatherIdentity(b, m)
	if request.clear {
		if identity == "" || p.db == nil {
			b.Send(m.ReplyTarget(), ircColor(ircRed, "weather defaults are unavailable right now"))
			return true
		}
		if err := p.clearWeatherDefault(identity); err != nil {
			b.Send(m.ReplyTarget(), ircColor(ircRed, "couldn't clear your weather default"))
			return true
		}
		b.Send(m.ReplyTarget(), ircColor(ircGreen, "weather default cleared"))
		return true
	}

	key := request.location
	if key == "" {
		if identity == "" || p.db == nil {
			b.Send(m.ReplyTarget(), weatherUsage())
			return true
		}
		key = p.savedWeatherDefault(identity)
		if key == "" {
			b.Send(m.ReplyTarget(), weatherUsage())
			return true
		}
	}
	cacheKey := strings.ToLower(key)
	p.mu.Lock()
	if v, ok := p.cache[cacheKey]; ok && time.Since(v.at) < 10*time.Minute {
		p.mu.Unlock()
		if request.setDefault {
			if err := p.saveWeatherDefault(identity, key); err != nil {
				b.Send(m.ReplyTarget(), ircColor(ircRed, "couldn't save your weather default"))
				return true
			}
			b.Send(m.ReplyTarget(), weatherDefaultSaved(v.location))
			return true
		}
		b.Send(m.ReplyTarget(), v.body)
		return true
	}
	p.mu.Unlock()
	var location struct {
		Results []struct {
			Name        string  `json:"name"`
			CountryCode string  `json:"country_code"`
			Latitude    float64 `json:"latitude"`
			Longitude   float64 `json:"longitude"`
		}
	}
	requestCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	geocodeURL := "https://geocoding-api.open-meteo.com/v1/search?name=" + url.QueryEscape(key) + "&count=1&language=en&format=json"
	req, _ := http.NewRequestWithContext(requestCtx, http.MethodGet, geocodeURL, nil)
	res, err := apiHTTPClient.Do(req)
	if err != nil || res.StatusCode != 200 {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "I couldn't find weather for that city."))
		return true
	}
	defer res.Body.Close()
	if err := json.NewDecoder(io.LimitReader(res.Body, 256<<10)).Decode(&location); err != nil || len(location.Results) == 0 {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "I couldn't find weather for that city."))
		return true
	}
	place := location.Results[0]
	units := strings.ToLower(p.cfg.String("default_units", "metric"))
	temperatureUnit, windUnit, temperatureSuffix := "celsius", "kmh", "°C"
	if units == "imperial" {
		temperatureUnit, windUnit, temperatureSuffix = "fahrenheit", "mph", "°F"
	}
	forecastURL := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%f&longitude=%f&current=temperature_2m,apparent_temperature,relative_humidity_2m,wind_speed_10m,wind_direction_10m,weather_code&temperature_unit=%s&wind_speed_unit=%s&timezone=auto", place.Latitude, place.Longitude, temperatureUnit, windUnit)
	req, _ = http.NewRequestWithContext(requestCtx, http.MethodGet, forecastURL, nil)
	res, err = apiHTTPClient.Do(req)
	if err != nil || res.StatusCode != 200 {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "Weather data is temporarily unavailable."))
		return true
	}
	defer res.Body.Close()
	var forecast struct {
		Current struct {
			Temperature   float64 `json:"temperature_2m"`
			FeelsLike     float64 `json:"apparent_temperature"`
			Humidity      float64 `json:"relative_humidity_2m"`
			WindSpeed     float64 `json:"wind_speed_10m"`
			WindDirection float64 `json:"wind_direction_10m"`
			WeatherCode   int     `json:"weather_code"`
		} `json:"current"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 256<<10)).Decode(&forecast); err != nil {
		b.Send(m.ReplyTarget(), ircColor(ircRed, "Weather data is temporarily unavailable."))
		return true
	}
	direction := compassDirection(forecast.Current.WindDirection)
	placeLabel := fmt.Sprintf("%s, %s", place.Name, strings.ToUpper(place.CountryCode))
	body := fmt.Sprintf("%s for %s: %.0f%s, %s, humidity %.0f%%, wind %.0f %s %s, feels like %.0f%s", ircColor(ircCyan, "Weather"), placeLabel, forecast.Current.Temperature, temperatureSuffix, weatherDescription(forecast.Current.WeatherCode), forecast.Current.Humidity, forecast.Current.WindSpeed, windUnit, direction, forecast.Current.FeelsLike, temperatureSuffix)
	p.mu.Lock()
	p.cache[cacheKey] = weatherCache{body: body, location: placeLabel, at: time.Now()}
	p.mu.Unlock()
	if request.setDefault {
		if err := p.saveWeatherDefault(identity, key); err != nil {
			b.Send(m.ReplyTarget(), ircColor(ircRed, "couldn't save your weather default"))
			return true
		}
		b.Send(m.ReplyTarget(), weatherDefaultSaved(placeLabel))
		return true
	}
	b.Send(m.ReplyTarget(), body)
	return true
}

type weatherRequest struct {
	location   string
	setDefault bool
	clear      bool
}

func parseWeatherRequest(arg string) (weatherRequest, bool) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return weatherRequest{}, true
	}
	fields := strings.Fields(arg)
	command := strings.ToLower(fields[0])
	rest := strings.TrimSpace(arg[len(fields[0]):])
	switch command {
	case "set", "default":
		location := stripWeatherQuotes(rest)
		if location == "" {
			return weatherRequest{}, false
		}
		return weatherRequest{location: location, setDefault: true}, true
	case "clear", "unset", "reset":
		if strings.TrimSpace(rest) != "" {
			return weatherRequest{}, false
		}
		return weatherRequest{clear: true}, true
	default:
		return weatherRequest{location: stripWeatherQuotes(arg)}, true
	}
}

func stripWeatherQuotes(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
			value = strings.TrimSpace(value[1 : len(value)-1])
		}
	}
	return value
}

func weatherUsage() string {
	return ircColor(ircYellow, "usage: !weather [city] | !weather set <city> | !weather clear")
}

func weatherIdentity(b *bot.Bot, m bot.Message) string {
	account := strings.TrimSpace(m.Account)
	if account != "" && account != "*" {
		return "account:" + strings.ToLower(account)
	}
	network := strings.ToLower(strings.TrimSpace(b.Config.NetworkName))
	nick := strings.ToLower(strings.TrimSpace(m.Nick))
	if network == "" || nick == "" {
		return ""
	}
	return "nick:" + network + "\x00" + nick
}

func (p *Weather) savedWeatherDefault(identity string) string {
	raw, err := p.db.Get(weatherDefaultsBucket, identity)
	if err != nil {
		return ""
	}
	var location string
	if storage.Decode(raw, &location) != nil {
		return ""
	}
	return strings.TrimSpace(location)
}

func (p *Weather) saveWeatherDefault(identity, location string) error {
	if p.db == nil || identity == "" {
		return fmt.Errorf("weather default storage is unavailable")
	}
	return p.db.Set(weatherDefaultsBucket, identity, strings.TrimSpace(location))
}

func (p *Weather) clearWeatherDefault(identity string) error {
	if p.db == nil || identity == "" {
		return fmt.Errorf("weather default storage is unavailable")
	}
	return p.db.Delete(weatherDefaultsBucket, identity)
}

func weatherDefaultSaved(location string) string {
	if strings.TrimSpace(location) == "" {
		location = "your saved location"
	}
	return ircColor(ircGreen, fmt.Sprintf("weather default saved as %s; use !weather or !wx anytime", location))
}

func isWeatherCommand(command string) bool {
	switch strings.ToLower(command) {
	case "weather", "wx", "forecast", "temp":
		return true
	default:
		return false
	}
}

func compassDirection(degrees float64) string {
	directions := []string{"N", "NE", "E", "SE", "S", "SW", "W", "NW"}
	index := int((degrees+22.5)/45) % len(directions)
	return directions[index]
}

func weatherDescription(code int) string {
	switch code {
	case 0:
		return "clear sky"
	case 1, 2:
		return "partly cloudy"
	case 3:
		return "overcast"
	case 45, 48:
		return "foggy"
	case 51, 53, 55, 56, 57:
		return "drizzle"
	case 61, 63, 65, 66, 67, 80, 81, 82:
		return "rain"
	case 71, 73, 75, 77, 85, 86:
		return "snow"
	case 95, 96, 99:
		return "thunderstorms"
	default:
		return "unknown conditions"
	}
}
