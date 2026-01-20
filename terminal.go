// terminal.go
// Управление интерактивным вводом с историей команд и навигацией

package main

import (
	"strings"
	"fmt"
	"sort"
	"sync"
	"bufio"
	"os"

	"github.com/peterh/liner"
)

// TerminalReader обрабатывает интерактивный ввод с историей команд
type TerminalReader struct {
	line        *liner.State
	history     []string  // Наша копия истории (для отладки/сохранения)
	maxHistory  int
	prompt      string
	mu 			sync.Mutex
}

func (t *TerminalReader) ClearHistory() error {
    t.history = make([]string, 0)
    // liner не поддерживает очистку, пересоздаем
    t.line.Close()
    t.line = liner.NewLiner()
    t.line.SetCtrlCAborts(true)
    return nil
}


// SetCompleter устанавливает функцию автодополнения для служебных команд
func (t *TerminalReader) SetCompleter(commands []string) {
	t.line.SetCompleter(func(line string) []string {
		if !strings.HasPrefix(line, ":") {
			return nil // Автодополнение только для команд
		}
		
		var completions []string
		for _, cmd := range commands {
			if strings.HasPrefix(cmd, line) {
				completions = append(completions, cmd)
			}
		}
		sort.Strings(completions)
		return completions
	})
}

// GetHistory возвращает копию истории команд
func (t *TerminalReader) GetHistory() []string {
	result := make([]string, len(t.history))
	copy(result, t.history)
	return result
}

// NewTerminalReader создает новый терминальный ридер с историей команд
func NewTerminalReader(prompt string, maxHistory int) *TerminalReader {
	line := liner.NewLiner()
	line.SetCtrlCAborts(true)  // Ctrl+C прерывает ввод, но не программу
	
	// Опционально: загрузка истории из файла при старте
	// line.ReadHistory("history.txt")
	
	return &TerminalReader{
		line:       line,
		history:    make([]string, 0, maxHistory),
		maxHistory: maxHistory,
		prompt:     prompt,
	}
}

// ReadLine читает строку с поддержкой редактирования и истории
func (t *TerminalReader) ReadLine() (string, error) {
	input, err := t.line.Prompt(t.prompt)
	if err != nil {
		return "", err
	}
	
	// Добавляем команду в историю liner (стрелки Вверх/Вниз)
	t.line.AppendHistory(input)
	
	// Ограничиваем внутреннюю историю
	if len(t.history) > t.maxHistory {
		t.line.Close()
		t.line = liner.NewLiner()
		t.line.SetCtrlCAborts(true)
		// Восстанавливаем последние команды
		for _, h := range t.history[len(t.history)-t.maxHistory:] {
			t.line.AppendHistory(h)
		}
	}
	
	// Сохраняем в нашу историю для совместимости
	if trimmed := strings.TrimSpace(input); trimmed != "" {
		if len(t.history) == 0 || t.history[len(t.history)-1] != input {
			t.history = append(t.history, input)
			if len(t.history) > t.maxHistory {
				t.history = t.history[1:]
			}
		}
	}
	
	return input, nil
}

// ReadLineWithPrompt читает строку с пользовательским промптом
func (t *TerminalReader) ReadLineWithPrompt(prompt string) (string, error) {
    // Убираем ведущие и завершающие символы новой строки и пробелы
    prompt = strings.TrimSpace(prompt)
    
    // Проверяем, что промпт не пустой
    if prompt == "" {
        prompt = "> "
    }
    
    input, err := t.line.Prompt(prompt)
    if err != nil {
        // Улучшаем обработку ошибки для отладки
        if err.Error() == "invalid prompt" || err.Error() == "invalid argument" {
            // Fallback: используем стандартный ввод если промпт вызывает ошибку
            fmt.Printf("%s", prompt)
            scanner := bufio.NewScanner(os.Stdin)
            if scanner.Scan() {
                return scanner.Text(), nil
            }
            return "", fmt.Errorf("ошибка ввода: %w", scanner.Err())
        }
        return "", fmt.Errorf("ошибка ввода: %w", err)
    }
    
    // Добавляем в историю
    if trimmed := strings.TrimSpace(input); trimmed != "" {
        t.line.AppendHistory(input)
        
        if len(t.history) == 0 || t.history[len(t.history)-1] != input {
            t.history = append(t.history, input)
            if len(t.history) > t.maxHistory {
                t.history = t.history[1:]
            }
        }
    }
    
    return input, nil
}

// Close освобождает ресурсы терминала
func (t *TerminalReader) Close() {
	// Опционально: сохранение истории в файл при выходе
	// t.line.WriteHistory("history.txt")
	t.line.Close()
}