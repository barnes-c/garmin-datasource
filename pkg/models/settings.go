package models

import (
	"encoding/json"
	"fmt"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

type PluginSettings struct {
	Email      string                `json:"email"`
	Secrets    *SecretPluginSettings `json:"-"`
	SpeedUnit  string                `json:"speedUnit"`  // kmh (default), mph, ms
	UnitSystem string                `json:"unitSystem"` // metric (default), imperial
}

type SecretPluginSettings struct {
	Password string `json:"password"`
	// Token is a Garmin OAuth token in the format produced by the /token
	// resource endpoint (or garmin_exporter's token file). When set, logins
	// resume this session instead of a fresh SSO round trip.
	Token string `json:"token"`
}

func LoadPluginSettings(source backend.DataSourceInstanceSettings) (*PluginSettings, error) {
	settings := PluginSettings{}
	err := json.Unmarshal(source.JSONData, &settings)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal PluginSettings json: %w", err)
	}

	settings.Secrets = loadSecretPluginSettings(source.DecryptedSecureJSONData)

	return &settings, nil
}

func loadSecretPluginSettings(source map[string]string) *SecretPluginSettings {
	return &SecretPluginSettings{
		Password: source["password"],
		Token:    source["token"],
	}
}
