// config.go
package main

import (
	"fmt"
	"strconv"
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	settings map[string]interface{}
}

const SessionFormatVersion = "1.0"

// В структуру сессии добавить поле Version
type SessionData struct {
    Version   string   `json:"version"`
    Timestamp string   `json:"timestamp"`
    Provider  string   `json:"provider"`
    Model     string   `json:"model"`
    Exchanges []string `json:"exchanges"`
}

func NewConfig() *Config {
	return &Config{
		settings: map[string]interface{}{
			"max_retries":   10,
			"web_search":    true,
			"debug_mode":    false,
			"context_limit": 10,
			"auto_execute":  false,
			"skip_install":  false,
		},
	}
}

// Save сохраняет конфигурацию в файл
func (c *Config) Save() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configDir := filepath.Join(home, ".cogitor")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}
	path := filepath.Join(configDir, "config.json")
	data, err := json.MarshalIndent(c.settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Load загружает конфигурацию из файла
func (c *Config) Load() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".cogitor", "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Файл не существует — используем дефолтные значения
		}
		return err
	}
	return json.Unmarshal(data, &c.settings)
}

func (c *Config) Set(key, value string) error {
	switch key {
	case "max_retries", "context_limit":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("недопустимое значение '%s': ожидается число", value)
		}
		// Валидация положительных значений
		if v <= 0 {
			return fmt.Errorf("значение для %s должно быть положительным числом", key)
		}
		// Дополнительная валидация для context_limit
		if key == "context_limit" && v > 100 {
			return fmt.Errorf("context_limit слишком большой (макс. 100)")
		}
		c.settings[key] = v
	case "web_search", "debug_mode", "auto_execute", "skip_install":
		// Унифицированная обработка булевых значений
		boolValue := value == "true" || value == "on" || value == "1" || value == "yes"
		c.settings[key] = boolValue
	default:
		return fmt.Errorf("неизвестная настройка: %s", key)
	}
	return nil
}

func (c *Config) Get(key string) (interface{}, bool) {
	v, ok := c.settings[key]
	return v, ok
}

// GetBool безопасно получаетbool значение
func (c *Config) GetBool(key string) bool {
	if v, ok := c.settings[key]; ok {
		switch val := v.(type) {
		case bool:
			return val
		case string:
			return val == "true" || val == "on" || val == "1" || val == "yes"	
		case float64: // Из JSON
			return val != 0
		}
	}
	return false
}

// GetInt безопасно получает int значение с fallback
func (c *Config) GetInt(key string, defaultValue int) int {
	if v, ok := c.settings[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case float64: // Из JSON
			return int(val)
		case string:
			if i, err := strconv.Atoi(val); err == nil {
				return i
			}
		}
	}
	return defaultValue
}

func (c *Config) GetAll() map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range c.settings {
		result[k] = v
	}
	return result
}

func (c *Config) Reset() {
	c.settings = map[string]interface{}{
		"max_retries":   10,
		"web_search":    true,
		"debug_mode":    false,
		"context_limit": 10,
		"auto_execute":  false,
		"skip_install":  false,
	}
}