package games

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode"
)

// ParseLaunchFields reads indexed JSON argv and environment objects. Legacy
// argument text supports quotes/escapes only; variables, commands and wildcards
// remain literal. No shell is executed. Prefer argv arrays for portable paths.
func ParseLaunchFields(arguments, environment string) ([]string, []string, error) {
	if len(arguments)+len(environment) > 32<<10 {
		return nil, nil, errors.New("launch metadata exceeds capacity")
	}
	var args []string
	var err error
	if text := strings.TrimSpace(arguments); strings.HasPrefix(text, "[") {
		var entries []json.RawMessage
		err = json.Unmarshal([]byte(text), &entries)
		if err == nil && len(entries) > 128 {
			return nil, nil, errors.New("too many game arguments")
		}
		for _, entry := range entries {
			var value string
			if len(entry) == 0 || entry[0] != '"' {
				return nil, nil, errors.New("game arguments must be strings")
			}
			if err := json.Unmarshal(entry, &value); err != nil {
				return nil, nil, err
			}
			args = append(args, value)
		}
	} else {
		args, err = literalArguments(arguments)
	}
	if err != nil {
		return nil, nil, err
	}
	var values map[string]json.RawMessage
	if text := strings.TrimSpace(environment); text != "" {
		if !strings.HasPrefix(text, "{") {
			return nil, nil, errors.New("game environment must be a JSON object")
		}
		if err := json.Unmarshal([]byte(text), &values); err != nil {
			return nil, nil, err
		}
	}
	keys := make([]string, 0, len(values))
	if len(values) > 128 {
		return nil, nil, errors.New("too many game environment values")
	}
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		entry := values[key]
		var value string
		if len(entry) == 0 || entry[0] != '"' {
			return nil, nil, errors.New("game environment values must be strings")
		}
		if err := json.Unmarshal(entry, &value); err != nil {
			return nil, nil, err
		}
		env = append(env, key+"="+value)
	}
	if err := validateLaunchValues(args, env); err != nil {
		return nil, nil, err
	}
	return args, env, nil
}

func literalArguments(text string) ([]string, error) {
	var args []string
	var word strings.Builder
	var quote rune
	present, escape := false, false
	for _, c := range text {
		if escape {
			if c == '\n' {
				escape = false
				continue
			}
			if quote == '"' && c != '"' && c != '\\' && c != '$' && c != '`' {
				word.WriteRune('\\')
			}
			word.WriteRune(c)
			present = true
			escape = false
			continue
		}
		if c == '\\' && quote != '\'' {
			escape = true
			present = true
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				word.WriteRune(c)
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			present = true
			continue
		}
		if unicode.IsSpace(c) {
			if present {
				args = append(args, word.String())
				word.Reset()
				present = false
			}
			continue
		}
		word.WriteRune(c)
		present = true
	}
	if quote != 0 || escape {
		return nil, errors.New("unclosed quote or escape in game arguments")
	}
	if present {
		args = append(args, word.String())
	}
	return args, nil
}

func validateLaunchValues(args, env []string) error {
	if len(args) > 128 || len(env) > 128 {
		return errors.New("too many game arguments/environment values")
	}
	total := 0
	for _, arg := range args {
		total += len(arg)
		if len(arg) > 4096 || strings.ContainsRune(arg, 0) {
			return errors.New("invalid game argument")
		}
	}
	for _, value := range env {
		total += len(value)
		key, _, ok := strings.Cut(value, "=")
		if !ok || len(value) > 4096 || strings.ContainsRune(value, 0) || key == "" || strings.EqualFold(key, "OMNI_SAVE_DIR") {
			return errors.New("invalid game environment")
		}
		for i, c := range key {
			if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9') {
				return errors.New("invalid environment name")
			}
		}
	}
	if total > 32<<10 {
		return errors.New("game arguments/environment exceed capacity")
	}
	return nil
}
