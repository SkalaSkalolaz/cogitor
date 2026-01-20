// markdown.go
package main

import (
	"fmt"
	// "os"      
	"strings"  
	"github.com/charmbracelet/glamour"
)

// RenderMarkdown рендерит Markdown в красивый ANSI-текст для терминала
func RenderMarkdown(content string) (string, error) {
	// Используем автоматическое определение темы терминала
	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(), // Автоматически выбирает светлую/тёмную тему
		glamour.WithWordWrap(115),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create renderer: %w", err)
	}

	rendered, err := renderer.Render(content)
	if err != nil {
		return "", fmt.Errorf("failed to render markdown: %w", err)
	}

	return rendered, nil
}

// IsMarkdownContent проверяет, содержит ли текст Markdown-разметку
func IsMarkdownContent(text string) bool {
	// Проверяем только если это НЕ блок кода
	if strings.Contains(text, "--- File:") {
		return false
	}

	markdownPatterns := []string{
		"```",      // Код
		"**",       // Жирный
		"*",        // Курсив
		"# ",       // Заголовки
		"- ",       // Списки
		"1. ",      // Нумерованные списки
		"[",        // Ссылки
		"> ",       // Цитаты
	}

	for _, pattern := range markdownPatterns {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}