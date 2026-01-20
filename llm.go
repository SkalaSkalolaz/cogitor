// llm.go
// Назначение: Интеграция с Large Language Models (ИИ-моделями) для автодополнения и анализа кода.
// Реализует взаимодействие с различными LLM-провайдерами (Ollama, Pollinations, OpenRouter и др.),
// обработку промптов, извлечение контента из ответов и потоковый режим работы.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
	"bufio"
    tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	fhttp "github.com/bogdanfinn/fhttp"
)

// Список поддерживаемых провайдеров LLM
var supportedProviders = []string{
	"ollama",
	"openrouter",
	"pollinations",
	"phind",
}

// Константы для провайдеров
const (
	phindEndpoint = "https://https.extension.phind.com/agent/"
)

func isURLLLM(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// extractContentFromLLMResponse универсально распознаёт текст или JSON-ответ LLM
// и извлекает текстовое содержимое ("content", "text" и т.д.).
func extractContentFromLLMResponse(body []byte) (string, error) {
	raw := strings.TrimSpace(string(body))
	if raw == "" {
		return "", errors.New("empty LLM response body")
	}

	if content, err := extractContentFromPossibleJSON(raw); err == nil && content != "" {
		return content, nil
	}

	type aiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Content string `json:"content"`
			Text    string `json:"text"`
			Delta   struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
		Text   string `json:"text"`
		Output string `json:"output"`
		Data   string `json:"data"`
	}

	var r aiResp
	if err := json.Unmarshal(body, &r); err == nil {
		if len(r.Choices) > 0 {
			choice := r.Choices[0]
			if choice.Message.Content != "" {
				return choice.Message.Content, nil
			}
			if choice.Delta.Content != "" {
				return choice.Delta.Content, nil
			}
			if choice.Content != "" {
				return choice.Content, nil
			}
			if choice.Text != "" {
				return choice.Text, nil
			}
		}
		if r.Text != "" {
			return r.Text, nil
		}
		if r.Output != "" {
			return r.Output, nil
		}
		if r.Data != "" {
			return r.Data, nil
		}
	}

	var simpleResp struct {
		Content string `json:"content"`
		Text    string `json:"text"`
		Message string `json:"message"`
		Result  string `json:"result"`
	}
	if err := json.Unmarshal(body, &simpleResp); err == nil {
		if simpleResp.Content != "" {
			return simpleResp.Content, nil
		}
		if simpleResp.Text != "" {
			return simpleResp.Text, nil
		}
		if simpleResp.Message != "" {
			return simpleResp.Message, nil
		}
		if simpleResp.Result != "" {
			return simpleResp.Result, nil
		}
	}

	return raw, nil
}

// extractContentFromPossibleJSON — улучшенный парсер LLM-ответов (распознаёт вложенный JSON, контент и text).
func extractContentFromPossibleJSON(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("empty response")
	}

	reFenced := regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)\\s*```")
	if m := reFenced.FindStringSubmatch(s); len(m) > 1 {
		s = strings.TrimSpace(m[1])
	}

	var obj interface{}
	if err := json.Unmarshal([]byte(s), &obj); err == nil {
		if content, ok := findContentRecursive(obj); ok {
			content = strings.TrimSpace(content)
			if content != "" {
				return content, nil
			}
		}
	}

	first := strings.IndexAny(s, "{[")
	lastBrace := strings.LastIndex(s, "}")
	lastBracket := strings.LastIndex(s, "]")
	last := lastBrace
	if lastBracket > last {
		last = lastBracket
	}

	if first != -1 && last > first {
		jsonStr := s[first : last+1]
		var innerObj interface{}
		if err := json.Unmarshal([]byte(jsonStr), &innerObj); err == nil {
			if content, ok := findContentRecursive(innerObj); ok {
				content = strings.TrimSpace(content)
				if content != "" {
					return content, nil
				}
			}
		}
	}

	return "", errors.New("no JSON content found")
}

// findContentRecursive ищет первое строковое поле "content"/"text" рекурсивно
func findContentRecursive(v interface{}) (string, bool) {
	switch t := v.(type) {
	case map[string]interface{}:
		// Сначала проверяем приоритетные поля
		priorityFields := []string{"content", "text", "message", "result", "output", "data"}
		for _, field := range priorityFields {
			if val, exists := t[field]; exists {
				if s, ok := val.(string); ok && strings.TrimSpace(s) != "" {
					return s, true
				}
			}
		}

		// Проверяем choices/chat_completion формат (OpenAI-совместимый)
		if choices, exists := t["choices"]; exists {
			if choicesSlice, ok := choices.([]interface{}); ok && len(choicesSlice) > 0 {
				if firstChoice, ok := choicesSlice[0].(map[string]interface{}); ok {
					// Проверяем message.content
					if message, exists := firstChoice["message"]; exists {
						if messageMap, ok := message.(map[string]interface{}); ok {
							if content, exists := messageMap["content"]; exists {
								if s, ok := content.(string); ok && strings.TrimSpace(s) != "" {
									return s, true
								}
							}
						}
					}
					// Проверяем delta.content (streaming)
					if delta, exists := firstChoice["delta"]; exists {
						if deltaMap, ok := delta.(map[string]interface{}); ok {
							if content, exists := deltaMap["content"]; exists {
								if s, ok := content.(string); ok && strings.TrimSpace(s) != "" {
									return s, true
								}
							}
						}
					}
					// Проверяем напрямую text/content
					if text, exists := firstChoice["text"]; exists {
						if s, ok := text.(string); ok && strings.TrimSpace(s) != "" {
							return s, true
						}
					}
					if content, exists := firstChoice["content"]; exists {
						if s, ok := content.(string); ok && strings.TrimSpace(s) != "" {
							return s, true
						}
					}
				}
			}
		}

		// Рекурсивно обходим остальные поля
		for _, val := range t {
			if s, ok := findContentRecursive(val); ok {
				return s, true
			}
		}

	case []interface{}:
		for _, item := range t {
			if s, ok := findContentRecursive(item); ok {
				return s, true
			}
		}

	case string:
		str := strings.TrimSpace(t)
		// Если строка выглядит как JSON, пробуем распарсить рекурсивно
		if (strings.HasPrefix(str, "{") && strings.HasSuffix(str, "}")) ||
			(strings.HasPrefix(str, "[") && strings.HasSuffix(str, "]")) {
			var inner interface{}
			if err := json.Unmarshal([]byte(str), &inner); err == nil {
				if s, ok := findContentRecursive(inner); ok {
					return s, true
				}
			}
		}
	}

	return "", false
}

func sendMessageToLLMUsingURL(ctx context.Context, endpoint, model, message, apiKey string) (string, error) {
	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": message},
		},
		"temperature": 0.2,
		"top_p":       1.0,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	if apiKey != "" {
		if strings.HasPrefix(apiKey, "sn-") {
			req.Header.Set("Authorization", apiKey)
		} else {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
	}

	// ДОБАВИТЬ: привязку контекста к запросу
	req = req.WithContext(ctx)
	
	// ДОБАВИТЬ: проверку отмены до выполнения запроса
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	ctxReq, cancel := context.WithTimeout(ctx, 240*time.Second)
	defer cancel()
	req = req.WithContext(ctxReq)

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("LLM URL request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := readWithContext(ctx, resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("LLM URL returned status %d: %s", resp.StatusCode, string(respBody))
	}

	content, err := extractContentFromLLMResponse(respBody)
	if err != nil {
		return "", err
	}
	return content, nil
}

func sendPhind(ctx context.Context, apiKeyArg, message, model string) (string, error) {
	// Phind не требует API ключ, но поддерживает если передан
	apiKey := apiKeyArg
	if apiKey == "" {
		apiKey = os.Getenv("PHIND_API_KEY")
	}

	// Формируем историю сообщений по спецификации Phind
	// Важно: первым сообщением должен быть system prompt
	messageHistory := []interface{}{
		map[string]interface{}{
			"role":    "system",
			"content": "You are a helpful assistant.", // Можно сделать настраиваемым через переменную окружения
		},
		map[string]interface{}{
			"role":    "user",
			"content": message,
		},
	}

	requestBody := map[string]interface{}{
		"additional_extension_context": "",
		"allow_magic_buttons":          true,
		"is_vscode_extension":          true,
		"requested_model":              model,
		"user_input":                   message,
		"message_history":              messageHistory,
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("phind: failed to marshal request body: %w", err)
	}
	req, err := fhttp.NewRequest("POST", phindEndpoint, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("phind: failed to create request: %w", err)
	}

	// Устанавливаем заголовки по спецификации Phind
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "") // Важно: пустой User-Agent
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "identity")

	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	// Привязываем контекст для отмены
	req = req.WithContext(ctx)
	
	// Проверяем отмену перед выполнением запроса
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	// Устанавливаем таймаут
	ctxReq, cancel := context.WithTimeout(ctx, 240*time.Second)
	defer cancel()
	req = req.WithContext(ctxReq)

	client, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), 
		tls_client.WithTimeoutSeconds(240),
		tls_client.WithClientProfile(profiles.Firefox_102),
	)
	if err != nil {
		return "", fmt.Errorf("phind: failed to create TLS client: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("phind: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("phind: status %d: %s", resp.StatusCode, string(respBody))
	}

	// Phind возвращает Server-Sent Events (SSE) формат
	// Парсим каждую строку "data: {...}"

	scanner := bufio.NewScanner(resp.Body)
    var fullContent strings.Builder
    
    for scanner.Scan() {
    	line := scanner.Text()
    	
    	// Пропускаем пустые строки
    	if strings.TrimSpace(line) == "" {
    		continue
    	}
    	
    	// Обрабатываем только SSE-строки
    	if !strings.HasPrefix(line, "data: ") {
    		continue
    	}
    	
    	jsonStr := strings.TrimPrefix(line, "data: ")
    	if jsonStr == "[DONE]" {
    		break
    	}
    	
    	// Парсим JSON
    	var data map[string]interface{}
    	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
    		continue
    	}
    	
    	// Проверяем структуру choices
    	if choices, ok := data["choices"].([]interface{}); ok && len(choices) > 0 {
    		choice, ok := choices[0].(map[string]interface{})
    		if !ok {
    			continue
    		}
    		
    		// Проверяем finish_reason
    		if finishReason, ok := choice["finish_reason"].(string); ok && finishReason == "stop" {
    			break
    		}
    		
    		// Извлекаем content из delta (streaming)
    		if delta, ok := choice["delta"].(map[string]interface{}); ok {
    			if content, ok := delta["content"].(string); ok && content != "" {
    				fullContent.WriteString(content)
    			}
    		}
    		
    		// Извлекаем content из message (обычный ответ)
    		if message, ok := choice["message"].(map[string]interface{}); ok {
    			if content, ok := message["content"].(string); ok && content != "" {
    				fullContent.WriteString(content)
    			}
    		}
    	}
    }	

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("phind: error reading response: %w", err)
	}

	return fullContent.String(), nil
}

func SendMessageToLLM(ctx context.Context, message, provider, model, apiKey string) (string, error) {
	if isURLLLM(provider) {
		result, err := sendMessageToLLMUsingURL(ctx, provider, model, message, apiKey)
		if err != nil {
			return "", fmt.Errorf("URL provider error: %w", err)
		}
		return result, nil
	}

	parsePollinationsResponse := func(body []byte) (string, error) {
		var m map[string]interface{}
		if err := json.Unmarshal(body, &m); err != nil {
			return "", fmt.Errorf("pollinations: invalid JSON: %w", err)
		}
		if t, ok := m["text"].(string); ok && t != "" {
			return t, nil
		}
		if c, ok := m["content"].(string); ok && c != "" {
			return c, nil
		}
		if choices, ok := m["choices"].([]interface{}); ok && len(choices) > 0 {
			if first, ok := choices[0].(map[string]interface{}); ok {
				if t, ok := first["text"].(string); ok && t != "" {
					return t, nil
				}
				if msg, ok := first["message"].(map[string]interface{}); ok {
					if t, ok := msg["content"].(string); ok && t != "" {
						return t, nil
					}
				}
			}
		}
		if out, ok := m["output"].(string); ok && out != "" {
			return out, nil
		}
		if data, ok := m["data"].(string); ok && data != "" {
			return data, nil
		}
		return "", errors.New("pollinations: could not recognize the response text")
	}

	parseOllamaResponse := func(body []byte) (string, error) {
		type ollamaChatMessage struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		type ollamaChoice struct {
			Message ollamaChatMessage `json:"message"`
		}
		type ollamaResponse struct {
			Choices []ollamaChoice `json:"choices"`
		}
		var r ollamaResponse
		if err := json.Unmarshal(body, &r); err == nil {
			if len(r.Choices) > 0 && r.Choices[0].Message.Content != "" {
				return r.Choices[0].Message.Content, nil
			}
		}
		var f map[string]interface{}
		if err := json.Unmarshal(body, &f); err == nil {
			if t, ok := f["text"].(string); ok && t != "" {
				return t, nil
			}
			if t, ok := f["data"].(string); ok && t != "" {
				return t, nil
			}
		}
		return "", errors.New("ollama: could not recognize the response text")
	}

	sendPollinations := func(ctx context.Context, apiKeyArg string) (string, error) {
		apiKey = apiKeyArg
		if apiKey == "" {
			apiKey = os.Getenv("POLLINATIONS_API_KEY")
		}
		url := "https://text.pollinations.ai/openai"
		type pollinationsMessage struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		type pollinationsRequestBody struct {
			Model    string                `json:"model"`
			Messages []pollinationsMessage `json:"messages"`
			Seed     int                   `json:"seed"`
		}

		body := pollinationsRequestBody{
			Model: model,
			Messages: []pollinationsMessage{
				{Role: "system", Content: "You are a helpful assistant."},
				{Role: "user", Content: message},
			},
			Seed: 42,
		}

		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return "", fmt.Errorf("pollinations: failed to construct the request body: %w", err)
		}

		req, err := http.NewRequest("POST", url, bytes.NewBuffer(bodyBytes))
		if err != nil {
			return "", fmt.Errorf("pollinations: failed to create the request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}

		// ДОБАВИТЬ: привязку контекста
		req = req.WithContext(ctx)
		
		// ДОБАВИТЬ: проверку отмены
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		ctxReq, cancel := context.WithTimeout(ctx, 240*time.Second)
		defer cancel()
		req = req.WithContext(ctxReq)

		client := &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
			},
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("pollinations: error net: %w", err)
		}
		defer resp.Body.Close()

		respBody, err := readWithContext(ctx, resp.Body)
		if err != nil {
			return "", fmt.Errorf("pollinations: failed to read the response: %w", err)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("pollinations: status %d: %s", resp.StatusCode, string(respBody))
		}
		parsed, err := parsePollinationsResponse(respBody)
		if err != nil {
			return "", fmt.Errorf("pollinations: failed to parse the response: %w", err)
		}
		return parsed, nil
	}

	sendOpenRouter := func(ctx context.Context, apiKeyArg string) (string, error) {
		baseURL := os.Getenv("OPENROUTER_BASE_URL")
		if baseURL == "" {
			baseURL = "https://openrouter.ai/api/v1"
		}
		apiKey = apiKeyArg
		if apiKey == "" {
			apiKey = os.Getenv("OPENROUTER_API_KEY")
		}
		url := baseURL + "/chat/completions"
		payload := map[string]interface{}{
			"model": model,
			"messages": []map[string]string{
				{"role": "user", "content": message},
			},
			"temperature": 0.2,
			"top_p":       1.0,
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return "", err
		}

		req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")

		if apiKey != "" {
			if strings.HasPrefix(apiKey, "sn-") {
				req.Header.Set("Authorization", apiKey)
			} else {
				req.Header.Set("Authorization", "Bearer "+apiKey)
			}
		}

		// ДОБАВИТЬ: привязку контекста
		req = req.WithContext(ctx)
		
		// ДОБАВИТЬ: проверку отмены
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		ctxReq, cancel := context.WithTimeout(ctx, 240*time.Second)
		defer cancel()
		req = req.WithContext(ctxReq)

		client := &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
			},
		}

		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("LLM URL request failed: %w", err)
		}
		defer resp.Body.Close()

		respBody, err := readWithContext(ctx, resp.Body)
		if err != nil {
			return "", err
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("LLM URL returned status %d: %s", resp.StatusCode, string(respBody))
		}

		content, err := extractContentFromLLMResponse(respBody)
		if err != nil {
			return "", err
		}
		return content, nil
	}


	sendOllama := func(ctx context.Context) (string, error) {
		url := "http://localhost:11434/v1/chat/completions"

		reqBody := map[string]interface{}{
			"model": model,
			"messages": []map[string]string{
				{"role": "user", "content": message},
			},
			"temperature": 0.2,
			"top_p":       1.0,
		}
		bodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			return "", fmt.Errorf("ollama: could not generate the request body: %w", err)
		}

		req, err := http.NewRequest("POST", url, bytes.NewBuffer(bodyBytes))
		if err != nil {
			return "", fmt.Errorf("ollama: failed to create the request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		// ДОБАВИТЬ: привязку контекста
		req = req.WithContext(ctx)
		
		// ДОБАВИТЬ: проверку отмены
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		ctxReq, cancel := context.WithTimeout(ctx, 480*time.Second)
		defer cancel()
		req = req.WithContext(ctxReq)

		client := &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
			},
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("ollama: error net: %w", err)
		}
		defer resp.Body.Close()

		respBody, err := readWithContext(ctx, resp.Body)
		if err != nil {
			return "", fmt.Errorf("ollama: reading the response failed: %w", err)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("ollama: status %d: %s", resp.StatusCode, string(respBody))
		}

		parsed, err := parseOllamaResponse(respBody)
		if err != nil {
			return "", fmt.Errorf("ollama: failed to parse the response: %w", err)
		}
		return parsed, nil
	}
	switch provider {
	case "pollinations":
		result, err := sendPollinations(ctx, apiKey)
		if err != nil {
			return "", fmt.Errorf("Pollinations error: %w", err)
		}
		content, parseErr := extractContentFromLLMResponse([]byte(result))
		if parseErr != nil {
			return "", fmt.Errorf("Pollinations response parsing error: %w", parseErr)
		}
		return content, nil
	case "openrouter":
		result, err := sendOpenRouter(ctx, apiKey)
		if err != nil {
			return "", fmt.Errorf("OpenRouter error: %w", err)
		}
		content, parseErr := extractContentFromLLMResponse([]byte(result))
		if parseErr != nil {
			return "", fmt.Errorf("OpenRouter response parsing error: %w", parseErr)
		}
		return content, nil
	case "ollama":
		result, err := sendOllama(ctx)
		if err != nil {
			return "", fmt.Errorf("Ollama error: %w", err)
		}
		content, parseErr := extractContentFromLLMResponse([]byte(result))
		if parseErr != nil {
			return "", fmt.Errorf("Ollama response parsing error: %w", parseErr)
		}
		return content, nil
	case "phind":
		result, err := sendPhind(ctx, apiKey, message, model)		
		if err != nil {
			return "", fmt.Errorf("Phind error: %w", err)
		}
		return result, nil

	default:
		return "", fmt.Errorf("unsupported provider: %s", provider)
	}
}

func nameModelPollinations() {
	resp, err := http.Get("https://text.pollinations.ai/models")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		return
	}

	var models []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	err = json.Unmarshal(body, &models)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("Pollinations models:\n")
	for _, model := range models {
		fmt.Printf(" %-40s  %s\n", model.Name, model.Description)
	}
}


func nameModelOpenRouter() {
	url := "https://openrouter.ai/api/v1/models"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println(err)
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()
	body, err := readWithContext(context.Background(), resp.Body)
	if err != nil {
		fmt.Println(err)
		return
	}

	type ApiMod struct {
		ID            string `json:"id"`
		ContextLength int    `json:"context_length"`
		Architecture  struct {
			InputModalities  []string `json:"input_modalities"`
			OutputModalities []string `json:"output_modalities"`
		} `json:"architecture"`
	}
	type DataWrapper struct {
		Data []ApiMod `json:"data"`
	}

	var dw DataWrapper
	if err := json.Unmarshal(body, &dw); err != nil {
		fmt.Println("Failed to parse the answer:", err)
		return
	}
	if len(dw.Data) == 0 {
		fmt.Println("No data of models")
		return
	}

	fmt.Printf("OpenRouter models:\n")
	for _, m := range dw.Data {
		in := "Not specified"
		if len(m.Architecture.InputModalities) > 0 {
			in = strings.Join(m.Architecture.InputModalities, ", ")
		}
		out := "Not specified"
		if len(m.Architecture.OutputModalities) > 0 {
			out = strings.Join(m.Architecture.OutputModalities, ", ")
		}
		fmt.Printf(" %-40s context=%d inputs=[%s] outputs=[%s]\n", m.ID, m.ContextLength, in, out)
	}
}

func ShowAvailableModels(provider string) error {
	switch provider {
	case "pollinations":
		nameModelPollinations()
	case "openrouter":
		nameModelOpenRouter()
	case "ollama":
		fmt.Println("ℹ️  Используйте `ollama list` для просмотра локальных моделей")
	case "phind":
		fmt.Println("ℹ️  Available models: Phind-70B, Phind-34B, Phind-CodeLlama-34B")
	default:
		return fmt.Errorf("неподдерживаемый провайдер")
	}
	return nil
}

// ShowAvailableProviders выводит список поддерживаемых провайдеров
func ShowAvailableProviders() {
	fmt.Println("🤖 Поддерживаемые провайдеры LLM:")
	for _, p := range supportedProviders {
		fmt.Printf("  - %s\n", p)
	}
	fmt.Println("\nДля подключения по URL используйте: :provider <url> <model> [api_key]")
	fmt.Println("Пример: :provider https://api.example.com gpt-4 mykey123")
}

// IsSupportedProvider проверяет, поддерживается ли провайдер
func IsSupportedProvider(name string) bool {
	// Проверяем стандартные провайдеры
	for _, p := range supportedProviders {
		if p == name {
			return true
		}
	}
	// Проверяем, является ли строка URL
	return isURLLLM(name)
}

func readWithContext(ctx context.Context, r io.Reader) ([]byte, error) {
    ch := make(chan []byte, 1)
    errCh := make(chan error, 1)
    go func() {
        b, err := io.ReadAll(r)
        if err != nil {
            errCh <- err
            return
        }
        ch <- b
    }()
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    case err := <-errCh:
        return nil, err
    case data := <-ch:
        return data, nil
    }
}