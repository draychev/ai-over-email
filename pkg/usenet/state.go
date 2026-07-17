package usenet

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type state struct {
	LastSeenNumber int               `json:"last_seen_number"`
	Replied        map[string]string `json:"replied"`
}

func loadState(path string) (state, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state{Replied: map[string]string{}}, nil
		}
		return state{}, fmt.Errorf("read state: %w", err)
	}
	var st state
	if err := json.Unmarshal(data, &st); err != nil {
		return state{}, fmt.Errorf("decode state: %w", err)
	}
	if st.Replied == nil {
		st.Replied = map[string]string{}
	}
	return st, nil
}

func saveState(path string, st state) error {
	if st.Replied == nil {
		st.Replied = map[string]string{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}
	return nil
}
