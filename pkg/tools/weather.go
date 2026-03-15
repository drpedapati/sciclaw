package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultWeatherForecastDays = 2
	maxWeatherForecastDays     = 7
)

type WeatherForecastTool struct {
	client          *http.Client
	geocodeBaseURL  string
	forecastBaseURL string
}

func NewWeatherForecastTool() *WeatherForecastTool {
	return &WeatherForecastTool{
		client:          &http.Client{Timeout: 12 * time.Second},
		geocodeBaseURL:  "https://geocoding-api.open-meteo.com",
		forecastBaseURL: "https://api.open-meteo.com",
	}
}

func newWeatherForecastToolForTest(geocodeBaseURL, forecastBaseURL string, client *http.Client) *WeatherForecastTool {
	tool := NewWeatherForecastTool()
	if strings.TrimSpace(geocodeBaseURL) != "" {
		tool.geocodeBaseURL = strings.TrimRight(strings.TrimSpace(geocodeBaseURL), "/")
	}
	if strings.TrimSpace(forecastBaseURL) != "" {
		tool.forecastBaseURL = strings.TrimRight(strings.TrimSpace(forecastBaseURL), "/")
	}
	if client != nil {
		tool.client = client
	}
	return tool
}

func (t *WeatherForecastTool) Name() string {
	return "weather_forecast"
}

func (t *WeatherForecastTool) Description() string {
	return "Get a structured weather forecast for a location without browsing. Prefer this over web_search or web_fetch for weather questions like current conditions, today, or tomorrow."
}

func (t *WeatherForecastTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"location": map[string]interface{}{
				"type":        "string",
				"description": "City, region, or place name, for example 'Cincinnati, OH' or 'London, UK'",
			},
			"day": map[string]interface{}{
				"type":        "string",
				"description": "Optional target day: 'today', 'tomorrow', or YYYY-MM-DD",
			},
			"days": map[string]interface{}{
				"type":        "integer",
				"description": fmt.Sprintf("Optional number of forecast days to return (default %d, max %d)", defaultWeatherForecastDays, maxWeatherForecastDays),
			},
			"unit_system": map[string]interface{}{
				"type":        "string",
				"description": "Unit system for temperatures and wind",
				"enum":        []string{"us", "metric"},
			},
		},
		"required": []string{"location"},
	}
}

func (t *WeatherForecastTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	location := strings.TrimSpace(getString(args, "location"))
	if location == "" {
		return ErrorResult("location is required")
	}

	days, err := getOptionalPositiveInt(args, "days", defaultWeatherForecastDays)
	if err != nil {
		return ErrorResult(err.Error())
	}
	if days > maxWeatherForecastDays {
		return ErrorResult(fmt.Sprintf("days must be between 1 and %d", maxWeatherForecastDays))
	}

	unitSystem := strings.TrimSpace(getString(args, "unit_system"))
	if unitSystem == "" {
		unitSystem = "us"
	}
	if unitSystem != "us" && unitSystem != "metric" {
		return ErrorResult("unit_system must be one of: us, metric")
	}

	day := strings.TrimSpace(strings.ToLower(getString(args, "day")))
	if day != "" && day != "today" && day != "tomorrow" && !isISODate(day) {
		return ErrorResult("day must be 'today', 'tomorrow', or YYYY-MM-DD")
	}
	if day == "tomorrow" && days < 2 {
		days = 2
	}

	place, err := t.lookupLocation(ctx, location)
	if err != nil {
		return ErrorResult(err.Error())
	}

	forecast, err := t.fetchForecast(ctx, place, days, unitSystem)
	if err != nil {
		return ErrorResult(err.Error())
	}

	targetIndex, targetLabel, err := pickForecastDay(forecast.Daily.Time, day)
	if err != nil {
		return ErrorResult(err.Error())
	}

	response := buildWeatherResponse(place, forecast, targetIndex, targetLabel, unitSystem)
	return NewToolResult(mustJSON(response))
}

type weatherLocation struct {
	Name        string
	Admin1      string
	Country     string
	CountryCode string
	Latitude    float64
	Longitude   float64
	Timezone    string
}

type weatherForecastResponse struct {
	CurrentUnits struct {
		Temperature2M    string `json:"temperature_2m"`
		ApparentTemp2M   string `json:"apparent_temperature"`
		WindSpeed10M     string `json:"wind_speed_10m"`
		RelativeHumidity string `json:"relative_humidity_2m"`
	} `json:"current_units"`
	Current struct {
		Time                string  `json:"time"`
		Temperature2M       float64 `json:"temperature_2m"`
		ApparentTemperature float64 `json:"apparent_temperature"`
		WeatherCode         int     `json:"weather_code"`
		WindSpeed10M        float64 `json:"wind_speed_10m"`
		RelativeHumidity2M  float64 `json:"relative_humidity_2m"`
	} `json:"current"`
	DailyUnits struct {
		Temperature2MMax     string `json:"temperature_2m_max"`
		Temperature2MMin     string `json:"temperature_2m_min"`
		PrecipProbabilityMax string `json:"precipitation_probability_max"`
		WindSpeed10MMax      string `json:"wind_speed_10m_max"`
	} `json:"daily_units"`
	Daily struct {
		Time                        []string  `json:"time"`
		WeatherCode                 []int     `json:"weather_code"`
		Temperature2MMax            []float64 `json:"temperature_2m_max"`
		Temperature2MMin            []float64 `json:"temperature_2m_min"`
		PrecipitationProbabilityMax []float64 `json:"precipitation_probability_max"`
		WindSpeed10MMax             []float64 `json:"wind_speed_10m_max"`
	} `json:"daily"`
}

func (t *WeatherForecastTool) lookupLocation(ctx context.Context, query string) (weatherLocation, error) {
	u, err := url.Parse(strings.TrimRight(t.geocodeBaseURL, "/") + "/v1/search")
	if err != nil {
		return weatherLocation{}, fmt.Errorf("failed to build weather geocoding request: %v", err)
	}
	params := u.Query()
	params.Set("name", query)
	params.Set("count", "1")
	params.Set("language", "en")
	params.Set("format", "json")
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return weatherLocation{}, fmt.Errorf("failed to create weather geocoding request: %v", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := t.client.Do(req)
	if err != nil {
		return weatherLocation{}, fmt.Errorf("weather geocoding request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return weatherLocation{}, fmt.Errorf("failed to read weather geocoding response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		return weatherLocation{}, fmt.Errorf("weather geocoding failed with HTTP %d", resp.StatusCode)
	}

	var payload struct {
		Results []struct {
			Name        string  `json:"name"`
			Admin1      string  `json:"admin1"`
			Country     string  `json:"country"`
			CountryCode string  `json:"country_code"`
			Latitude    float64 `json:"latitude"`
			Longitude   float64 `json:"longitude"`
			Timezone    string  `json:"timezone"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return weatherLocation{}, fmt.Errorf("failed to parse weather geocoding response: %v", err)
	}
	if len(payload.Results) == 0 {
		return weatherLocation{}, fmt.Errorf("no weather location matched %q", query)
	}

	result := payload.Results[0]
	return weatherLocation{
		Name:        result.Name,
		Admin1:      result.Admin1,
		Country:     result.Country,
		CountryCode: result.CountryCode,
		Latitude:    result.Latitude,
		Longitude:   result.Longitude,
		Timezone:    result.Timezone,
	}, nil
}

func (t *WeatherForecastTool) fetchForecast(ctx context.Context, place weatherLocation, days int, unitSystem string) (weatherForecastResponse, error) {
	u, err := url.Parse(strings.TrimRight(t.forecastBaseURL, "/") + "/v1/forecast")
	if err != nil {
		return weatherForecastResponse{}, fmt.Errorf("failed to build weather forecast request: %v", err)
	}

	params := u.Query()
	params.Set("latitude", strconv.FormatFloat(place.Latitude, 'f', 4, 64))
	params.Set("longitude", strconv.FormatFloat(place.Longitude, 'f', 4, 64))
	params.Set("timezone", "auto")
	params.Set("forecast_days", strconv.Itoa(days))
	params.Set("current", strings.Join([]string{
		"temperature_2m",
		"apparent_temperature",
		"relative_humidity_2m",
		"weather_code",
		"wind_speed_10m",
	}, ","))
	params.Set("daily", strings.Join([]string{
		"weather_code",
		"temperature_2m_max",
		"temperature_2m_min",
		"precipitation_probability_max",
		"wind_speed_10m_max",
	}, ","))
	if unitSystem == "metric" {
		params.Set("temperature_unit", "celsius")
		params.Set("wind_speed_unit", "kmh")
		params.Set("precipitation_unit", "mm")
	} else {
		params.Set("temperature_unit", "fahrenheit")
		params.Set("wind_speed_unit", "mph")
		params.Set("precipitation_unit", "inch")
	}
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return weatherForecastResponse{}, fmt.Errorf("failed to create weather forecast request: %v", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := t.client.Do(req)
	if err != nil {
		return weatherForecastResponse{}, fmt.Errorf("weather forecast request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return weatherForecastResponse{}, fmt.Errorf("failed to read weather forecast response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		return weatherForecastResponse{}, fmt.Errorf("weather forecast failed with HTTP %d", resp.StatusCode)
	}

	var forecast weatherForecastResponse
	if err := json.Unmarshal(body, &forecast); err != nil {
		return weatherForecastResponse{}, fmt.Errorf("failed to parse weather forecast response: %v", err)
	}
	if len(forecast.Daily.Time) == 0 {
		return weatherForecastResponse{}, fmt.Errorf("weather forecast response was empty")
	}
	return forecast, nil
}

func buildWeatherResponse(place weatherLocation, forecast weatherForecastResponse, targetIndex int, targetLabel, unitSystem string) map[string]interface{} {
	locationLabelParts := []string{place.Name}
	if strings.TrimSpace(place.Admin1) != "" {
		locationLabelParts = append(locationLabelParts, place.Admin1)
	}
	if strings.TrimSpace(place.Country) != "" {
		locationLabelParts = append(locationLabelParts, place.Country)
	}
	locationLabel := strings.Join(locationLabelParts, ", ")

	targetDate := forecast.Daily.Time[targetIndex]
	targetSummary := map[string]interface{}{
		"date":                           targetDate,
		"label":                          targetLabel,
		"conditions":                     weatherCodeLabel(forecast.Daily.WeatherCode[targetIndex]),
		"weather_code":                   forecast.Daily.WeatherCode[targetIndex],
		"temperature_max":                forecast.Daily.Temperature2MMax[targetIndex],
		"temperature_min":                forecast.Daily.Temperature2MMin[targetIndex],
		"temperature_unit":               forecast.DailyUnits.Temperature2MMax,
		"precipitation_probability_max":  forecast.Daily.PrecipitationProbabilityMax[targetIndex],
		"precipitation_probability_unit": forecast.DailyUnits.PrecipProbabilityMax,
		"wind_speed_max":                 forecast.Daily.WindSpeed10MMax[targetIndex],
		"wind_speed_unit":                forecast.DailyUnits.WindSpeed10MMax,
	}

	days := make([]map[string]interface{}, 0, len(forecast.Daily.Time))
	for i := range forecast.Daily.Time {
		days = append(days, map[string]interface{}{
			"date":                          forecast.Daily.Time[i],
			"conditions":                    weatherCodeLabel(forecast.Daily.WeatherCode[i]),
			"weather_code":                  forecast.Daily.WeatherCode[i],
			"temperature_max":               forecast.Daily.Temperature2MMax[i],
			"temperature_min":               forecast.Daily.Temperature2MMin[i],
			"precipitation_probability_max": forecast.Daily.PrecipitationProbabilityMax[i],
			"wind_speed_max":                forecast.Daily.WindSpeed10MMax[i],
		})
	}

	summary := fmt.Sprintf("%s in %s: %s with a high of %.0f%s, low of %.0f%s, precipitation chance up to %.0f%s, and winds up to %.0f%s.",
		titleWeatherLabel(targetLabel),
		locationLabel,
		weatherCodeLabel(forecast.Daily.WeatherCode[targetIndex]),
		forecast.Daily.Temperature2MMax[targetIndex],
		forecast.DailyUnits.Temperature2MMax,
		forecast.Daily.Temperature2MMin[targetIndex],
		forecast.DailyUnits.Temperature2MMin,
		forecast.Daily.PrecipitationProbabilityMax[targetIndex],
		forecast.DailyUnits.PrecipProbabilityMax,
		forecast.Daily.WindSpeed10MMax[targetIndex],
		forecast.DailyUnits.WindSpeed10MMax,
	)

	current := map[string]interface{}{
		"time":                      forecast.Current.Time,
		"conditions":                weatherCodeLabel(forecast.Current.WeatherCode),
		"weather_code":              forecast.Current.WeatherCode,
		"temperature":               forecast.Current.Temperature2M,
		"temperature_unit":          forecast.CurrentUnits.Temperature2M,
		"apparent_temperature":      forecast.Current.ApparentTemperature,
		"apparent_temperature_unit": forecast.CurrentUnits.ApparentTemp2M,
		"relative_humidity":         forecast.Current.RelativeHumidity2M,
		"relative_humidity_unit":    forecast.CurrentUnits.RelativeHumidity,
		"wind_speed":                forecast.Current.WindSpeed10M,
		"wind_speed_unit":           forecast.CurrentUnits.WindSpeed10M,
	}

	return map[string]interface{}{
		"provider":    "open-meteo",
		"summary":     summary,
		"unit_system": unitSystem,
		"location": map[string]interface{}{
			"name":         place.Name,
			"admin1":       place.Admin1,
			"country":      place.Country,
			"country_code": place.CountryCode,
			"latitude":     place.Latitude,
			"longitude":    place.Longitude,
			"timezone":     place.Timezone,
			"label":        locationLabel,
		},
		"target_day": targetSummary,
		"current":    current,
		"daily":      days,
	}
}

func pickForecastDay(dates []string, day string) (int, string, error) {
	if len(dates) == 0 {
		return 0, "", fmt.Errorf("weather forecast response was empty")
	}
	switch day {
	case "":
		if len(dates) > 1 {
			return 1, "tomorrow", nil
		}
		return 0, "today", nil
	case "today":
		return 0, "today", nil
	case "tomorrow":
		if len(dates) < 2 {
			return 0, "", fmt.Errorf("tomorrow is not available in the returned forecast window")
		}
		return 1, "tomorrow", nil
	default:
		for i, date := range dates {
			if date == day {
				label := "forecast day"
				if i == 0 {
					label = "today"
				} else if i == 1 {
					label = "tomorrow"
				}
				return i, label, nil
			}
		}
		return 0, "", fmt.Errorf("requested date %s is outside the returned forecast window", day)
	}
}

func isISODate(v string) bool {
	if len(v) != len("2006-01-02") {
		return false
	}
	_, err := time.Parse("2006-01-02", v)
	return err == nil
}

func weatherCodeLabel(code int) string {
	switch code {
	case 0:
		return "clear sky"
	case 1:
		return "mainly clear"
	case 2:
		return "partly cloudy"
	case 3:
		return "overcast"
	case 45, 48:
		return "fog"
	case 51, 53, 55:
		return "drizzle"
	case 56, 57:
		return "freezing drizzle"
	case 61, 63, 65:
		return "rain"
	case 66, 67:
		return "freezing rain"
	case 71, 73, 75, 77:
		return "snow"
	case 80, 81, 82:
		return "rain showers"
	case 85, 86:
		return "snow showers"
	case 95:
		return "thunderstorm"
	case 96, 99:
		return "thunderstorm with hail"
	default:
		return fmt.Sprintf("weather code %d", code)
	}
}

func titleWeatherLabel(label string) string {
	trimmed := strings.TrimSpace(label)
	if trimmed == "" {
		return "Forecast"
	}
	return strings.ToUpper(trimmed[:1]) + trimmed[1:]
}
