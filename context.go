// context.go
package main

import (
	"strings"
	"fmt"
	"encoding/json"
	"sync"
)

const (
	DefaultMaxLength = 10
	MaxExchangeSize  = 100000 // 100KB на один exchange
	MaxTotalSize     = 500000 // 500KB на весь контекст
)

// ContextManager управляет контекстом разговора
type ContextManager struct {
	conversation []string
	maxLength    int
	totalSize    int
	mu           sync.RWMutex
}

// NewContextManager создает новый менеджер контекста
func NewContextManager() *ContextManager {
	return &ContextManager{
		conversation: make([]string, 0),
		maxLength:    DefaultMaxLength,
		totalSize:    0,
	}
}

// AddExchange добавляет обмен в контекст
func (cm *ContextManager) AddExchange(question, answer string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	// Создаем exchange с ограничением размера
	rawExchange := "Вопрос: " + question + "\nОтвет: " + answer
	exchange := cm.truncateExchange(rawExchange)
	exchangeSize := len(exchange)
	
	// Добавляем новый exchange
	cm.conversation = append(cm.conversation, exchange)
	cm.totalSize += exchangeSize
	
	// Ограничиваем по количеству
	if len(cm.conversation) > cm.maxLength {
		removed := cm.conversation[0]
		cm.conversation = cm.conversation[1:]
		cm.totalSize -= len(removed)
	}
	
	// Ограничиваем по общему размеру (более агрессивно)
	cm.enforceTotalSizeLimit()
}

// truncateExchange обрезает слишком длинные exchange
func (cm *ContextManager) truncateExchange(exchange string) string {
	if len(exchange) <= MaxExchangeSize {
		return exchange
	}
	// Обрезаем и добавляем метку для отладки
	return exchange[:MaxExchangeSize] + "\n... [обрезано из-за ограничения размера]"
}

// enforceTotalSizeLimit удаляет старые exchange при превышении лимита
func (cm *ContextManager) enforceTotalSizeLimit() {
	// Удаляем из начала пока общий размер не станет меньше лимита
	for cm.totalSize > MaxTotalSize && len(cm.conversation) > 1 {
		removed := cm.conversation[0]
		cm.conversation = cm.conversation[1:]
		cm.totalSize -= len(removed)
	}
}

// GetContext возвращает текущий контекст
func (cm *ContextManager) GetContext() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if len(cm.conversation) == 0 {
		return ""
	}
	
	return "Предыдущие обмены:\n" + strings.Join(cm.conversation, "\n\n") + "\n\n"
}

// Clear очищает контекст
func (cm *ContextManager) Clear() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.conversation = make([]string, 0)
	cm.totalSize = 0
	cm.maxLength = DefaultMaxLength
}

// Pop удаляет последние n обменов
func (cm *ContextManager) Pop(n int) error {
    cm.mu.Lock()
	defer cm.mu.Unlock()

	if n <= 0 {
		return fmt.Errorf("количество должно быть положительным числом")
	}
	if n > len(cm.conversation) {
		return fmt.Errorf("в контексте только %d обменов", len(cm.conversation))
	}
	
	// Удаляем из конца и обновляем totalSize (для n > 1)
	start := len(cm.conversation) - n
	for i := start; i < len(cm.conversation); i++ {
		cm.totalSize -= len(cm.conversation[i])
	}
	
	cm.conversation = cm.conversation[:start]
	return nil
}

// GetExchangeCount возвращает количество обменов
func (cm *ContextManager) GetExchangeCount() int {
	return len(cm.conversation)
}

func (cm *ContextManager) GetMaxLength() int {
	return cm.maxLength
}

// GetEstimatedTokens приблизительно оценивает токены (1 токен ≈ 3 символа)
func (cm *ContextManager) GetEstimatedTokens() int {
    cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Теперь это точная оценка на основе отслеживаемого размера
	// Учитываем, что 1 токен ≈ 3-4 символа (с запасом)
	return cm.totalSize / 3
}

// SetMaxLength изменяет размер окна контекста
func (cm *ContextManager) SetMaxLength(maxLength int) error {
	if maxLength <= 0 {
		return fmt.Errorf("лимит должен быть положительным числом")
	}
	if maxLength > 100 {
		return fmt.Errorf("слишком большой лимит (максимум 100)")
	}
	cm.maxLength = maxLength
	
	// Ограничиваем по количеству
	for len(cm.conversation) > cm.maxLength {
		removed := cm.conversation[0]
		cm.conversation = cm.conversation[1:]
		cm.totalSize -= len(removed)
	}
	
	// Также проверяем общий размер
	cm.enforceTotalSizeLimit()
	return nil
}

// GetAllExchanges возвращает все обмены (для :save)
func (cm *ContextManager) GetAllExchanges() []string {
	result := make([]string, len(cm.conversation))
	copy(result, cm.conversation)
	return result
}

// LoadFromHistory загружает контекст из массива обменов (для :load)
func (cm *ContextManager) LoadFromHistory(exchanges []string) {
	cm.Clear()
	cm.conversation = exchanges
	
	// Пересчитываем totalSize
	cm.totalSize = 0
	for _, exchange := range cm.conversation {
		cm.totalSize += len(exchange)
	}
	
	// Применяем ограничения
	if len(cm.conversation) > cm.maxLength {
		cm.conversation = cm.conversation[len(cm.conversation)-cm.maxLength:]
		// Пересчитываем totalSize после обрезки
		cm.totalSize = 0
		for _, exchange := range cm.conversation {
			cm.totalSize += len(exchange)
		}
	}
	
	// Окончательная проверка по размеру
	cm.enforceTotalSizeLimit()
}

// ToJSON возвращает JSON-представление контекста
func (cm *ContextManager) ToJSON() string {
	data, err := json.Marshal(map[string]interface{}{
		"exchanges": cm.conversation,
		"maxLength": cm.maxLength,
	})
	if err != nil {
		return `{"exchanges": [], "error": "serialization_failed"}`
	}
	return string(data)
}