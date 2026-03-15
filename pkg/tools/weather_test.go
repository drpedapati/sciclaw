package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWeatherForecastToolExecute_DefaultsToTomorrow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/search":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"results": [{
					"name": "Cincinnati",
					"admin1": "Ohio",
					"country": "United States",
					"country_code": "US",
					"latitude": 39.1031,
					"longitude": -84.5120,
					"timezone": "America/New_York"
				}]
			}`))
		case "/v1/forecast":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"current_units": {
					"temperature_2m": "°F",
					"apparent_temperature": "°F",
					"relative_humidity_2m": "%",
					"wind_speed_10m": "mph"
				},
				"current": {
					"time": "2026-03-15T09:00",
					"temperature_2m": 54,
					"apparent_temperature": 52,
					"relative_humidity_2m": 61,
					"weather_code": 2,
					"wind_speed_10m": 8
				},
				"daily_units": {
					"temperature_2m_max": "°F",
					"temperature_2m_min": "°F",
					"precipitation_probability_max": "%",
					"wind_speed_10m_max": "mph"
				},
				"daily": {
					"time": ["2026-03-15", "2026-03-16"],
					"weather_code": [1, 2],
					"temperature_2m_max": [57, 61],
					"temperature_2m_min": [41, 45],
					"precipitation_probability_max": [10, 35],
					"wind_speed_10m_max": [9, 14]
				}
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tool := newWeatherForecastToolForTest(server.URL, server.URL, server.Client())
	result := tool.Execute(context.Background(), map[string]interface{}{
		"location": "Cincinnati, OH",
	})
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(result.ForLLM), &payload); err != nil {
		t.Fatalf("unmarshal result: %v\nraw: %s", err, result.ForLLM)
	}

	if payload["provider"] != "open-meteo" {
		t.Fatalf("provider=%v", payload["provider"])
	}
	if payload["summary"] == "" || !strings.Contains(payload["summary"].(string), "Tomorrow in Cincinnati, Ohio, United States") {
		t.Fatalf("unexpected summary: %v", payload["summary"])
	}
	targetDay := payload["target_day"].(map[string]interface{})
	if targetDay["label"] != "tomorrow" {
		t.Fatalf("target_day.label=%v", targetDay["label"])
	}
	if targetDay["date"] != "2026-03-16" {
		t.Fatalf("target_day.date=%v", targetDay["date"])
	}
}

func TestWeatherForecastToolExecute_RejectsOutOfWindowDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/search":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"results": [{
					"name": "Cincinnati",
					"admin1": "Ohio",
					"country": "United States",
					"country_code": "US",
					"latitude": 39.1031,
					"longitude": -84.5120,
					"timezone": "America/New_York"
				}]
			}`))
		case "/v1/forecast":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"current_units": {
					"temperature_2m": "°F",
					"apparent_temperature": "°F",
					"relative_humidity_2m": "%",
					"wind_speed_10m": "mph"
				},
				"current": {
					"time": "2026-03-15T09:00",
					"temperature_2m": 54,
					"apparent_temperature": 52,
					"relative_humidity_2m": 61,
					"weather_code": 2,
					"wind_speed_10m": 8
				},
				"daily_units": {
					"temperature_2m_max": "°F",
					"temperature_2m_min": "°F",
					"precipitation_probability_max": "%",
					"wind_speed_10m_max": "mph"
				},
				"daily": {
					"time": ["2026-03-15", "2026-03-16"],
					"weather_code": [1, 2],
					"temperature_2m_max": [57, 61],
					"temperature_2m_min": [41, 45],
					"precipitation_probability_max": [10, 35],
					"wind_speed_10m_max": [9, 14]
				}
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tool := newWeatherForecastToolForTest(server.URL, server.URL, server.Client())
	result := tool.Execute(context.Background(), map[string]interface{}{
		"location": "Cincinnati, OH",
		"day":      "2026-03-20",
	})
	if !result.IsError {
		t.Fatalf("expected error for out-of-window date, got: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "outside the returned forecast window") {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
}
