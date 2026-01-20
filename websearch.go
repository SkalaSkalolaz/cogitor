// websearch.go
// Назначение: Поиск информации в интернете через DuckDuckGo для получения актуальных данных

package main

import (
	"fmt"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
	"net/http"
	"net/url"
	"strings"
	"time"
	"context"
)

// Link представляет найденную ссылку
type Link struct {
	Title string
	URL   string
}

// SearchResult представляет результат поиска с оценкой достоверности
type SearchResult struct {
	Query      string
	Content    string
	Sources    []Link
	Confidence int    // 0-100, оценка достоверности
	Summary    string // краткое описание содержания
}

// LogSearchRequest логирует запрос к поиску для пользователя
func LogSearchRequest(query, reason string) {

	fmt.Printf("🌐 LLM запросил веб-поиск: \"%s\"\n", query)
    fmt.Printf("   Причина: %s\n", reason)
    fmt.Println("   Поиск текущей информации...")
}


// normalizeWhitespace collapses all whitespace (spaces, tabs, newlines) into single spaces.
func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// FetchTopText takes a search query, fetches the top 5 DuckDuckGo HTML results,
// retrieves visible text from each page, and returns the concatenated text.
func FetchTopText(ctx context.Context, query string) (*SearchResult, error) {
	fmt.Printf("🌐 Поиск: %s\n", query)
	

	links, err := fetchTopLinks(ctx, query)
	if err != nil {
		return nil, err
	}

	var texts []string
	var sources []Link
	limit := 5
	if len(links) < limit {
		limit = len(links)
	}
	
	for i := 0; i < limit; i++ {
		fmt.Printf("📄 Загрузка содержимого из: %s\n", links[i].URL)

		t, err := fetchTextFromURLGoDuckSearch(ctx, links[i].URL)
		if err != nil {
			// Пропускаем страницу при ошибке чтения, продолжая с остальными
			fmt.Printf("⚠️  Ошибка при получении %s: %v\n", links[i].URL, err)
			continue
		}
		t = strings.TrimSpace(t)
		if t != "" {
			texts = append(texts, t)
			sources = append(sources, links[i])
		}
	}

	if len(texts) == 0 {
		return nil, fmt.Errorf("no content found for query: %s", query)
	}

	combined := strings.Join(texts, "\n\n")
	combined = normalizeWhitespace(combined)
	
	// Оцениваем достоверность на основе количества источников и содержания
	confidence := estimateConfidence(sources, combined)
	
	result := &SearchResult{
		Query:      query,
		Content:    combined,
		Sources:    sources,
		Confidence: confidence,
		Summary:    generateSummary(combined),
	}
	
	fmt.Printf("✅ Поиск завершен: %d источников, уверенность: %d%%\n", len(sources), confidence)
	return result, nil
}

// fetchTopLinks запрашивает DuckDuckGo HTML-версию поиска и возвращает первые 5 ссылок.
func fetchTopLinks(ctx context.Context, query string) ([]Link, error) {
	escaped := url.QueryEscape(query)
	searchURL := "https://duckduckgo.com/html/?q=" + escaped

	client := &http.Client{
		Timeout: 15 * time.Second,
	}
	
	// ✅ ИСПОЛЬЗУЕМ NewRequestWithContext
	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; AI-Code-Assistant/1.0; +https://github.com/aicode-assistant)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP status: %d", resp.StatusCode)
	}

	// Декодируем в UTF-8 согласно заголовку Content-Type
	reader, err := charset.NewReader(resp.Body, resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}
	doc, err := html.Parse(reader)
	if err != nil {
		return nil, err
	}

	var links []Link
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			var href string
			var class string
			for _, a := range n.Attr {
				if a.Key == "href" {
					href = a.Val
				}
				if a.Key == "class" {
					class = a.Val
				}
			}
			// DuckDuckGo результаты часто имеют класс "result__a"
			if strings.Contains(class, "result__a") && href != "" {
				title := extractText(n)
				finalURL := href
				// Попытка извлечь финальный URL через uddg (если есть)
				if u, err := url.Parse(href); err == nil {
					if q, err := url.ParseQuery(u.RawQuery); err == nil {
						if uddg := q.Get("uddg"); uddg != "" {
							if decoded, err := url.QueryUnescape(uddg); err == nil {
								finalURL = decoded
							} else {
								finalURL = uddg
							}
						}
					}
					// если итоговый URL всё ещё относительный, преобразуем в абсолютный DDG URL
					if strings.HasPrefix(finalURL, "/") {
						finalURL = "https://duckduckgo.com" + finalURL
					}
				}
				// если это всё ещё не http(s), попытаться сделать полный URL через DDG базу
				if !strings.HasPrefix(finalURL, "http://") && !strings.HasPrefix(finalURL, "https://") && strings.HasPrefix(href, "/") {
					finalURL = "https://duckduckgo.com" + href
				}
				// пропустим ссылки на сам DDG
				if strings.Contains(finalURL, "duckduckgo.com") {
					// ничего не делаем
				} else {
					links = append(links, Link{Title: strings.TrimSpace(title), URL: finalURL})
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)

	// Ограничиваем первыми 5 ссылками
	if len(links) > 5 {
		links = links[:5]
	}
	return links, nil
}

// fetchTextFromURLGoDuckSearch загружает страницу и возвращает её видимый текст.
func fetchTextFromURLGoDuckSearch(ctx context.Context, pageURL string) (string, error) {
	client := &http.Client{
		Timeout: 20 * time.Second,
	}
	
	// ✅ ИСПОЛЬЗУЕМ NewRequestWithContext
	req, err := http.NewRequestWithContext(ctx, "GET", pageURL, nil)
	if err != nil {
		return "", err
	}
	
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; AI-Code-Assistant/1.0; +https://github.com/aicode-assistant)")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	// Простой ответ-обработчик
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP status: %d", resp.StatusCode)
	}

	// Декодируем в UTF-8
	reader, err := charset.NewReader(resp.Body, resp.Header.Get("Content-Type"))
	if err != nil {
		return "", err
	}
	doc, err := html.Parse(reader)
	if err != nil {
		return "", err
	}

	text := extractText(doc)
	// normalize whitespace
	text = normalizeWhitespace(text)
	return text, nil
}

// extractText рекурсивно собирает видимый текст из HTML-узла.
func extractText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		// Игнорируем скрипты и стили
		if node.Type == html.ElementNode && (node.Data == "script" || node.Data == "style" || node.Data == "nav" || node.Data == "header" || node.Data == "footer") {
			return
		}
		if node.Type == html.TextNode {
			// Убираем лишние пробелы
			text := strings.TrimSpace(node.Data)
			if text != "" {
				b.WriteString(text)
				b.WriteByte(' ')
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// estimateConfidence оценивает достоверность найденной информации
func estimateConfidence(sources []Link, content string) int {
	// Базовая оценка на основе количества источников
	confidence := len(sources) * 15
	if confidence > 60 {
		confidence = 60
	}

	// Проверяем качество контента
	content = strings.ToLower(content)
	
	// Положительные признаки
	positiveSignals := []string{
		"official", "documentation", "github.com", "stackoverflow", "w3.org", ".gov",
		"mozilla", "developer", "tutorial", "guide", "example", "sample",
		"официальный", "документация", "github.com", "stackoverflow", "w3.org",
		"mozilla", "производитель", "описание", "руководство", "пример", "образец",
			
	}
	
	// Отрицательные признаки
	negativeSignals := []string{
		"click here", "download now", "buy now", "limited time", "advertisement",
		"sponsored", "popup", "sign up", "subscribe", "спонсор",
	}
	
	for _, signal := range positiveSignals {
		if strings.Contains(content, signal) {
			confidence += 5
		}
	}
	
	for _, signal := range negativeSignals {
		if strings.Contains(content, signal) {
			confidence -= 10
		}
	}
	
	// Ограничиваем диапазон
	if confidence < 10 {
		confidence = 10
	}
	if confidence > 95 {
		confidence = 95
	}
	
	return confidence
}

// generateSummary генерирует краткое описание контента
func generateSummary(content string) string {
	if len(content) > 500 {
		return content[:500] + "..."
	}
	return content
}

// ShouldSearch определяет, нужен ли поиск в интернете для данного вопроса
func ShouldSearch(question, language string) (bool, string) {
	lowerQuestion := strings.ToLower(question)

	normalizedQuestion := " " + lowerQuestion + " "

	// Фразы, которые ЗАПРЕЩАЮТ поиск (проверяем в первую очередь!)
	searchBlockingPhrases := []string{
		"напиши код",
		"напиши программу", 
		"создай файл",
		"перепиши код",
		"измени код",
		"добавь функцию",
		"реализуй",
		// Добавляем английские аналоги для полноты
		"write code",
		"write program",
		"create file",
		"rewrite code",
		"modify code",
		"add function",
		"implement",
	}

	// Проверяем блокирующие фразы
	for _, phrase := range searchBlockingPhrases {
		// Более точное сопоставление с учетом границ слов
		if strings.Contains(normalizedQuestion, " "+phrase+" ") ||
			strings.HasPrefix(lowerQuestion, phrase+" ") ||
			strings.HasSuffix(lowerQuestion, " "+phrase) {
			// return false, "no_search_needed"
			return false, "search_blocked_by_phrase"
		}
	}


	// Темы, требующие актуальной информации
	topicsNeedingCurrentInfo := []string{
		"latest", "recent", "current", "new", "update", "version",
		"today", "2024", "2025", "2026", "modern", "trend", "best practice", "найди в интернете",
		"recent change", "new feature", "release", "deprecated",
		"последний", "текущий", "новый", "новости", "обновление", "версия",
		"сегодня", "современный", "тренд", "лучшая практика",
		"недавние изменения", "новая функция", "выпуск", "устарело",
	}
	
	// Конкретные технические темы, требующие поиска
	technicalTopics := []string{
		"how to", "tutorial", "guide", "example", "sample code",
		"documentation", "api reference", "library", "package",
		"framework", "tool", "installation", "setup", "configuration",
		"руководство",
		"документация", "ссылка на API", "библиотека", "пакет",
		"фреймворк", "инструмент", "установка", "настройка", "конфигурация",
	}
	
	// Проверяем, нужна ли актуальная информация
	for _, topic := range topicsNeedingCurrentInfo {
		if strings.Contains(lowerQuestion, topic) {
			return true, "question_requires_current_info"
		}
	}
	
	// Проверяем технические темы
	for _, topic := range technicalTopics {
		if strings.Contains(lowerQuestion, topic) {
			return true, "technical_topic_requires_docs"
		}
	}
	
	// Специфичные для языков программирования темы
	languageSpecificSearch := map[string][]string{
		"go": {"go mod", "go get", "goroutine", "channel", "interface", "struct"},
		"python": {"pip install", "virtualenv", "decorator", "list comprehension", 
		          "pandas", "numpy", "django", "flask"},
		"javascript": {"npm install", "react", "vue", "angular", "node.js", 
		             "express", "webpack", "babel"},
		"java": {"maven", "gradle", "spring", "hibernate", "jpa", "servlet"},
	}
	
	if topics, exists := languageSpecificSearch[language]; exists {
		for _, topic := range topics {
			if strings.Contains(lowerQuestion, strings.ToLower(topic)) {
				return true, "language_specific_topic"
			}
		}
	}
	
	return false, "no_search_needed"
}