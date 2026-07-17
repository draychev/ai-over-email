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
	Usenet UsenetConfig `json:"usenet"`
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

type UsenetConfig struct {
	Host              string `json:"host"`
	Port              int    `json:"port"`
	Security          string `json:"security"`
	TLSServerName     string `json:"tls_server_name"`
	TLSCertSHA256     string `json:"tls_cert_sha256"`
	Group             string `json:"group"`
	PollInterval      string `json:"poll_interval"`
	StatePath         string `json:"state_path"`
	FromName          string `json:"from_name"`
	FromAddress       string `json:"from_address"`
	MaxThreadArticles int    `json:"max_thread_articles"`
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
	if cfg.Usenet.Host != "" || cfg.Usenet.Group != "" {
		if strings.TrimSpace(cfg.Usenet.Host) == "" {
			return fmt.Errorf("config field usenet.host is required when usenet is configured")
		}
		if cfg.Usenet.Port < 0 || cfg.Usenet.Port > 65535 {
			return fmt.Errorf("config field usenet.port must be between 0 and 65535")
		}
		if cfg.Usenet.Security != "" {
			switch strings.ToLower(strings.TrimSpace(cfg.Usenet.Security)) {
			case "tls", "none":
			default:
				return fmt.Errorf("config field usenet.security must be tls or none")
			}
		}
		if strings.TrimSpace(cfg.Usenet.Group) == "" {
			return fmt.Errorf("config field usenet.group is required when usenet is configured")
		}
		if cfg.Usenet.MaxThreadArticles < 0 {
			return fmt.Errorf("config field usenet.max_thread_articles must be non-negative")
		}
		if cfg.Usenet.FromAddress != "" {
			if _, err := parseConfigEmail(cfg.Usenet.FromAddress); err != nil {
				return fmt.Errorf("config field usenet.from_address contains invalid email %q: %w", cfg.Usenet.FromAddress, err)
			}
		}
		if cfg.Usenet.TLSCertSHA256 != "" {
			if err := validateSHA256Fingerprint("usenet.tls_cert_sha256", cfg.Usenet.TLSCertSHA256); err != nil {
				return err
			}
		}
	}
	return nil
}

func (cfg UsenetConfig) Normalized() UsenetConfig {
	cfg.Host = strings.TrimSpace(cfg.Host)
	cfg.Security = strings.ToLower(strings.TrimSpace(cfg.Security))
	cfg.TLSServerName = strings.TrimSpace(cfg.TLSServerName)
	cfg.TLSCertSHA256 = normalizeFingerprint(cfg.TLSCertSHA256)
	cfg.Group = strings.TrimSpace(cfg.Group)
	cfg.PollInterval = strings.TrimSpace(cfg.PollInterval)
	cfg.StatePath = strings.TrimSpace(cfg.StatePath)
	cfg.FromName = strings.TrimSpace(cfg.FromName)
	cfg.FromAddress = strings.TrimSpace(cfg.FromAddress)
	if cfg.Port == 0 {
		if cfg.Security == "none" {
			cfg.Port = 119
		} else {
			cfg.Port = 563
		}
	}
	if cfg.Security == "" {
		if cfg.Port == 119 && cfg.TLSServerName == "" && cfg.TLSCertSHA256 == "" {
			cfg.Security = "none"
		} else {
			cfg.Security = "tls"
		}
	}
	if cfg.PollInterval == "" {
		cfg.PollInterval = "1m"
	}
	if cfg.StatePath == "" {
		cfg.StatePath = ".tmp/usenetwatch-state.json"
	}
	if cfg.FromName == "" {
		cfg.FromName = "Pegasus AI"
	}
	if cfg.FromAddress == "" {
		cfg.FromAddress = "pegasus-ai@localhost"
	}
	if cfg.MaxThreadArticles == 0 {
		cfg.MaxThreadArticles = 40
	}
	return cfg
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

func validateSHA256Fingerprint(field, value string) error {
	normalized := normalizeFingerprint(value)
	if len(normalized) != 64 {
		return fmt.Errorf("config field %s must be a SHA-256 fingerprint", field)
	}
	for _, r := range normalized {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return fmt.Errorf("config field %s must be a SHA-256 fingerprint", field)
		}
	}
	return nil
}

func normalizeFingerprint(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "sha256 fingerprint=")
	value = strings.TrimPrefix(value, "sha256=")
	value = strings.ReplaceAll(value, ":", "")
	value = strings.ReplaceAll(value, " ", "")
	return value
}
