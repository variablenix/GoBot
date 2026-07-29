package plugins

import (
	"context"
	"encoding/json"
	"fmt"
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
	mu    sync.Mutex
	cache map[string]weatherCache
}

var apiHTTPClient = &http.Client{Timeout: 10 * time.Second}

type weatherCache struct {
	body string
	at   time.Time
}

func (p *Weather) Name() string       { return "weather" }
func (p *Weather) Commands() []string { return []string{"weather"} }
func (p *Weather) Help() string {
	return "!weather <city> — current weather from Open-Meteo (no API key required)"
}
func (p *Weather) Init(c bot.PluginConfig, _ *storage.DB) error {
	p.cfg = c
	p.cache = map[string]weatherCache{}
	return nil
}
func (p *Weather) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok || cmd != "weather" {
		return false
	}
	key := strings.TrimSpace(arg)
	if key == "" {
		b.Send(m.ReplyTarget(), "usage: !weather <city>")
		return true
	}
	p.mu.Lock()
	if v, ok := p.cache[strings.ToLower(key)]; ok && time.Since(v.at) < 10*time.Minute {
		p.mu.Unlock()
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
		b.Send(m.ReplyTarget(), "I couldn't find weather for that city.")
		return true
	}
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(&location); err != nil || len(location.Results) == 0 {
		b.Send(m.ReplyTarget(), "I couldn't find weather for that city.")
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
		b.Send(m.ReplyTarget(), "Weather data is temporarily unavailable.")
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
	if err := json.NewDecoder(res.Body).Decode(&forecast); err != nil {
		b.Send(m.ReplyTarget(), "Weather data is temporarily unavailable.")
		return true
	}
	direction := compassDirection(forecast.Current.WindDirection)
	body := fmt.Sprintf("Weather for %s, %s: %.0f%s, %s, humidity %.0f%%, wind %.0f %s %s, feels like %.0f%s", place.Name, strings.ToUpper(place.CountryCode), forecast.Current.Temperature, temperatureSuffix, weatherDescription(forecast.Current.WeatherCode), forecast.Current.Humidity, forecast.Current.WindSpeed, windUnit, direction, forecast.Current.FeelsLike, temperatureSuffix)
	p.mu.Lock()
	p.cache[strings.ToLower(key)] = weatherCache{body, time.Now()}
	p.mu.Unlock()
	b.Send(m.ReplyTarget(), body)
	return true
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
