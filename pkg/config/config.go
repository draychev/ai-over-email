package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
)

type ConfigStruct struct {
	JMAP JMAPConfig `json:"jmap"`
}

type JMAPConfig struct {
	SessionEndpoint                string `json:"session_endpoint"`
	LegacyBasicAuthSessionEndpoint string `json:"legacy_basic_auth_session_endpoint"`
}

func Load(path string) (ConfigStruct, error) {
	file, err := os.Open(path)
	if err != nil {
		return ConfigStruct{}, fmt.Errorf("open config: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()

	var cfg ConfigStruct
	if err := decoder.Decode(&cfg); err != nil {
		return ConfigStruct{}, fmt.Errorf("decode config: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return ConfigStruct{}, fmt.Errorf("decode config: %w", err)
		}
		return ConfigStruct{}, fmt.Errorf("decode config: multiple JSON values")
	}
	if err := cfg.Validate(); err != nil {
		return ConfigStruct{}, err
	}

	return cfg, nil
}

func (cfg ConfigStruct) Validate() error {
	if err := validateHTTPSURL("jmap.session_endpoint", cfg.JMAP.SessionEndpoint); err != nil {
		return err
	}
	if err := validateHTTPSURL("jmap.legacy_basic_auth_session_endpoint", cfg.JMAP.LegacyBasicAuthSessionEndpoint); err != nil {
		return err
	}
	return nil
}

func validateHTTPSURL(field, value string) error {
	if value == "" {
		return fmt.Errorf("config field %s is required", field)
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil {
		return fmt.Errorf("config field %s must be a valid URL: %w", field, err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("config field %s must use https", field)
	}
	if parsed.Host == "" {
		return fmt.Errorf("config field %s must include a host", field)
	}
	return nil
}
