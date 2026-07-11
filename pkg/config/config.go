package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net/mail"
	"net/url"
	"os"
	"strings"
)

const (
	DefaultOpenAIModel           = "gpt-5-nano"
	DefaultOpenAIReasoningEffort = "high"
)

type ConfigStruct struct {
	JMAP   JMAPConfig   `json:"jmap"`
	OpenAI OpenAIConfig `json:"openai"`
}

type JMAPConfig struct {
	SessionEndpoint                string `json:"session_endpoint"`
	LegacyBasicAuthSessionEndpoint string `json:"legacy_basic_auth_session_endpoint"`
}

type OpenAIConfig struct {
	DefaultModel            string   `json:"default_model"`
	DefaultReasoningEffort  string   `json:"default_reasoning_effort"`
	PowerfulModel           string   `json:"powerful_model"`
	PowerfulReasoningEffort string   `json:"powerful_reasoning_effort"`
	PowerfulSenders         []string `json:"powerful_senders"`
}

type OpenAIModelSettings struct {
	Model           string
	ReasoningEffort string
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
	if err := validateReasoningEffort("openai.default_reasoning_effort", cfg.OpenAI.defaultReasoningEffort()); err != nil {
		return err
	}
	if err := validateReasoningEffort("openai.powerful_reasoning_effort", cfg.OpenAI.powerfulReasoningEffort()); err != nil {
		return err
	}
	for _, sender := range cfg.OpenAI.PowerfulSenders {
		if _, err := parseConfigEmail(sender); err != nil {
			return fmt.Errorf("config field openai.powerful_senders contains invalid email %q: %w", sender, err)
		}
	}
	return nil
}

func (cfg ConfigStruct) OpenAISettingsForSenders(senders []string) OpenAIModelSettings {
	defaults := OpenAIModelSettings{
		Model:           cfg.OpenAI.defaultModel(),
		ReasoningEffort: cfg.OpenAI.defaultReasoningEffort(),
	}
	powerful := OpenAIModelSettings{
		Model:           cfg.OpenAI.powerfulModel(),
		ReasoningEffort: cfg.OpenAI.powerfulReasoningEffort(),
	}
	if len(cfg.OpenAI.PowerfulSenders) == 0 {
		return defaults
	}

	allowed := make(map[string]struct{}, len(cfg.OpenAI.PowerfulSenders))
	for _, sender := range cfg.OpenAI.PowerfulSenders {
		email, err := parseConfigEmail(sender)
		if err != nil {
			continue
		}
		allowed[email] = struct{}{}
	}
	for _, sender := range senders {
		email, err := parseConfigEmail(sender)
		if err != nil {
			continue
		}
		if _, ok := allowed[email]; ok {
			return powerful
		}
	}
	return defaults
}

func (cfg OpenAIConfig) defaultModel() string {
	if model := strings.TrimSpace(cfg.DefaultModel); model != "" {
		return model
	}
	return DefaultOpenAIModel
}

func (cfg OpenAIConfig) defaultReasoningEffort() string {
	if effort := strings.TrimSpace(cfg.DefaultReasoningEffort); effort != "" {
		return strings.ToLower(effort)
	}
	return DefaultOpenAIReasoningEffort
}

func (cfg OpenAIConfig) powerfulModel() string {
	if model := strings.TrimSpace(cfg.PowerfulModel); model != "" {
		return model
	}
	return cfg.defaultModel()
}

func (cfg OpenAIConfig) powerfulReasoningEffort() string {
	if effort := strings.TrimSpace(cfg.PowerfulReasoningEffort); effort != "" {
		return strings.ToLower(effort)
	}
	return cfg.defaultReasoningEffort()
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

func validateReasoningEffort(field, value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "minimal", "low", "medium", "high":
		return nil
	default:
		return fmt.Errorf("config field %s must be one of minimal, low, medium, high", field)
	}
}

func parseConfigEmail(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("empty email")
	}
	parsed, err := mail.ParseAddress(value)
	if err != nil {
		return "", err
	}
	return strings.ToLower(parsed.Address), nil
}
