// assistant.go
// Основной модуль взаимодействия с пользователем, обработки запросов и координации LLM

package main

import (
	// "bufio"
	ctx "context"   
	status "context"
	"fmt"
	"os"
	"os/signal"   
	"path/filepath"
	"strings"
	"sync"   
	"syscall"   
	"errors"
	"io"
	"time"
	"github.com/peterh/liner"
)

// AssistantAPI определяет интерфейс для взаимодействия с Assistant
// Это разрывает циклическую зависимость между Assistant и CommandHandler
type AssistantAPI interface {
	GetContext() *ContextManager
	GetFileParser() *FileParser
	GetCodeRunner() *CodeRunner
	GetCodeParser() *CodeParser
	GetInstaller() *Installer
	GetTerminalReader() *TerminalReader
	GetDiffProcessor() *DiffProcessor
	GetProvider() string
	GetModel() string
	GetAPIKey() string
	GetLastUserQuery() string
	GetConfig() *Config
	SetModel(model string)
	SetProvider(provider, model, apiKey string)
	ProcessQuery(query string, autoMode bool)
}

// Гарантируем, что Assistant реализует AssistantAPI
var _ AssistantAPI = (*Assistant)(nil)

// Assistant представляет собой интеллектуального ассистента
type Assistant struct {
	provider         string
	model            string
	apiKey           string
	webSearchEnabled bool
	context          *ContextManager
	fileParser       *FileParser
	codeRunner       *CodeRunner
	codeParser       *CodeParser
	installer        *Installer
	terminalReader   *TerminalReader
	commandHandler   *CommandHandler
	lastUserQuery    string
	diffProcessor    *DiffProcessor
	requestMu        sync.Mutex
	requestCtx       ctx.Context
	requestCancel    ctx.CancelFunc
    ragData        []RAGDocument
    ragEnabled     bool
    ragMutex       sync.RWMutex
	autoCopyEnabled bool
}

// Добавляем структуру для RAG-документов:
type RAGDocument struct {
    FilePath    string
    Content     string
    Size        int
    LoadedAt    time.Time
}

// Добавляем методы для работы с RAG-данными:
func (a *Assistant) GetRAGData() []RAGDocument {
    a.ragMutex.RLock()
    defer a.ragMutex.RUnlock()
    return a.ragData
}

func (a *Assistant) SetRAGData(docs []RAGDocument) {
    a.ragMutex.Lock()
    defer a.ragMutex.Unlock()
    a.ragData = docs
    a.ragEnabled = len(docs) > 0
}

func (a *Assistant) ClearRAGData() {
    a.ragMutex.Lock()
    defer a.ragMutex.Unlock()
    a.ragData = []RAGDocument{}
    a.ragEnabled = false
}

func (a *Assistant) IsRAGEnabled() bool {
    a.ragMutex.RLock()
    defer a.ragMutex.RUnlock()
    return a.ragEnabled
}

func (a *Assistant) GetRAGContext() string {
    a.ragMutex.RLock()
    defer a.ragMutex.RUnlock()
    
    if !a.ragEnabled || len(a.ragData) == 0 {
        return ""
    }
    
    var context strings.Builder
    context.WriteString("\n=== ИНФОРМАЦИЯ ИЗ ФАЙЛОВ ДАННЫХ (RAG) ===\n")
    context.WriteString("Используй ТОЛЬКО эту информацию для ответа на вопросы:\n\n")
    
    totalSize := 0
    for i, doc := range a.ragData {
        // Ограничиваем размер каждого документа для контекста
        maxDocSize := 5000
        content := doc.Content
        if len(content) > maxDocSize {
            content = content[:maxDocSize] + "... [обрезано]"
        }
        
        context.WriteString(fmt.Sprintf("--- Документ %d: %s (%d символов) ---\n", 
            i+1, filepath.Base(doc.FilePath), doc.Size))
        context.WriteString(content)
        context.WriteString("\n\n")
        
        totalSize += len(content)
        
        // Ограничиваем общий размер RAG-контекста
        if totalSize > 15000 {
            context.WriteString("...[остальные документы не помещаются в контекст]...\n")
            break
        }
    }
    
    context.WriteString("ИНСТРУКЦИИ:\n")
    context.WriteString("1. Используй ТОЛЬКО предоставленные данные из файлов\n")
    context.WriteString("2. Не добавляй информацию из своих знаний\n")
    context.WriteString("3. Если данных недостаточно - честно скажи об этом\n")
    context.WriteString("4. Соотноси запрос пользователя с данными из файлов\n")
    
    return context.String()
}

// SetModel временно изменяет модель для текущей сессии
func (a *Assistant) SetModel(model string) {
	a.model = model
	if a.isDebugMode() {
		fmt.Printf("🔧 Модель обновлена: %s\n", model)
	}
}

// SetProvider временно изменяет провайдера, модель и API ключ для текущей сессии
func (a *Assistant) SetProvider(provider, model, apiKey string) {
	a.provider = provider
	a.model = model
	if apiKey != "" {
		a.apiKey = apiKey
	}
	if a.isDebugMode() {
		fmt.Printf("🔧 Провайдер обновлен: %s | Модель: %s\n", provider, model)
	}
}

// Реализация AssistantAPI: геттеры для доступа к компонентам

func (a *Assistant) GetContext() *ContextManager {
	return a.context
}

func (a *Assistant) GetFileParser() *FileParser {
	return a.fileParser
}

func (a *Assistant) GetCodeRunner() *CodeRunner {
	return a.codeRunner
}

func (a *Assistant) GetCodeParser() *CodeParser {
	return a.codeParser
}

func (a *Assistant) GetInstaller() *Installer {
	return a.installer
}


func (a *Assistant) GetTerminalReader() *TerminalReader {
	return a.terminalReader
}

func (a *Assistant) GetDiffProcessor() *DiffProcessor {
	return a.diffProcessor
}

func (a *Assistant) GetProvider() string {
	return a.provider
}

func (a *Assistant) GetModel() string {
	return a.model
}

func (a *Assistant) GetAPIKey() string {
	return a.apiKey
}

func (a *Assistant) GetLastUserQuery() string {
	return a.lastUserQuery
}

func (a *Assistant) GetConfig() *Config {
	if a.commandHandler != nil {
		return a.commandHandler.config
	}
	return NewConfig()
}

// SetAutoCopyEnabled включает/выключает автоматическое копирование ответов в буфер обмена
func (a *Assistant) SetAutoCopyEnabled(enabled bool) {
    a.autoCopyEnabled = enabled
    if a.isDebugMode() {
        status := "выключено"
        if enabled {
            status = "включено"
        }
        fmt.Printf("🔧 Авто-копирование ответов: %s\n", status)
    }
}

// GetAutoCopyEnabled возвращает статус авто-копирования
func (a *Assistant) GetAutoCopyEnabled() bool {
    return a.autoCopyEnabled
}

func NewAssistant(provider, model, apiKey string, webSearchEnabled bool) *Assistant {
	terminalReader := NewTerminalReader("👤 Вы: ", 20)

	// ✅ Создаем и ЗАГРУЖАЕМ конфигурацию
	config := NewConfig()
	if err := config.Load(); err != nil {
		fmt.Printf("⚠️  Не удалось загрузить конфиг: %v\n", err)
	}

	stats := NewStatistics()
	fileParser := NewFileParser()

	// Создаем раннер с конфигом
	codeRunner := NewCodeRunner(config)

	// Синхронизируем context_limit при старте
	contextManager := NewContextManager()
	if limit := config.GetInt("context_limit", 10); limit > 0 {
		contextManager.SetMaxLength(limit)
	}

	// Создаем COMPLETELY инициализированный assistant
	assistant := &Assistant{
		provider:         provider,
		model:            model,
		apiKey:           apiKey,
		webSearchEnabled: webSearchEnabled,
		context:          contextManager,
		fileParser:       fileParser,
		codeRunner:       codeRunner,
		codeParser:       NewCodeParser(),
		installer:        NewInstaller(terminalReader, config),
		terminalReader:   terminalReader,
        diffProcessor:    NewDiffProcessor(fileParser, terminalReader, config),
		lastUserQuery:    "",
		ragData:        []RAGDocument{},
		ragEnabled:     false,
        // autoCopyEnabled: config.GetBool("auto_copy_responses", false),
		autoCopyEnabled: false,
	}
	
	// Теперь создаем CommandHandler с Assistant как AssistantAPI
	assistant.commandHandler = NewCommandHandler(assistant, config, stats, terminalReader)

	// После создания commandHandler устанавливаем правильное значение
	assistant.autoCopyEnabled = assistant.getConfigBoolSafe("auto_copy_responses", false)
	
	return assistant
}

// getConfigBoolSafe безопасно получает bool значение из конфига с значением по умолчанию
func (a *Assistant) getConfigBoolSafe(key string, defaultValue bool) bool {
    if a.commandHandler == nil || a.commandHandler.config == nil {
        return defaultValue
    }
    
    val, ok := a.commandHandler.config.Get(key)
    if !ok {
        return defaultValue
    }
    
    // Преобразуем различные типы к bool
    switch v := val.(type) {
    case bool:
        return v
    case string:
        return strings.ToLower(v) == "true" || v == "1"
    case int, int64:
        // Для числовых типов: 0 = false, != 0 = true
        return v != 0
    default:
        return defaultValue
    }
}

// handleExplicitInternetSearch обрабатывает запросы вида "Найди в интернете {запрос}"
// Возвращает очищенный запрос и true, если поиск был выполнен
func (a *Assistant) handleExplicitInternetSearch(query string, context *string) (string, bool) {
    // Проверяем начало запроса (оба регистра)
    lowerQuery := strings.ToLower(query)
    pattern := "найди в интернете"
    
    if !strings.HasPrefix(lowerQuery, pattern) {
        return query, false
    }
    
    // Извлекаем запрос после паттерна
	searchQuery := strings.TrimSpace(query[len(pattern):])

    // Убираем запятую в начале, если есть
    if len(searchQuery) > 0 && searchQuery[0] == ',' {
        searchQuery = strings.TrimSpace(searchQuery[1:])
    }
    
    if searchQuery == "" {
        fmt.Println("⚠️ Пустой поисковый запрос после 'Найди в интернете'")
        return query, false
    }
    
    // --- НОВАЯ ЛОГИКА РАЗДЕЛЕНИЯ ЗАПРОСА ---
    internetQuery := searchQuery
    llmAdditional := ""
    
    if colonIndex := strings.Index(searchQuery, ":"); colonIndex != -1 {
        // Часть до двоеточия - поисковый запрос
        internetQuery = strings.TrimSpace(searchQuery[:colonIndex])
        // Часть после двоеточия - дополнения для LLM
        llmPart := searchQuery[colonIndex:]
        llmAdditional = strings.Replace(llmPart, ":", ",", 1)
        
        if a.isDebugMode() {
            fmt.Printf("🔍 Разделенный запрос - Поиск: '%s' | Для LLM: '%s'\n", internetQuery, llmAdditional)
        }
    }
    
    fmt.Printf("🌐 Выполняю поиск в интернете: %s\n", internetQuery)   
    // Выполняем поиск через существующую функцию
	searchResult, err := FetchTopText(a.requestCtx, internetQuery)
    if err != nil {
        fmt.Printf("⚠️ Поиск не удался: %v\n", err)
        *context += "\n[Поиск в интернете не удался]\n"
        return searchQuery, true
    }
    
    // Добавляем результаты в контекст с детальными инструкциями для LLM
	*context += fmt.Sprintf("\n=== ПОИСК В ИНТЕРНЕТЕ (запрос: '%s') ===\n", internetQuery)
    *context += searchResult.Summary
    *context += "\nИсточники: " + a.formatSources(searchResult.Sources)
    *context += fmt.Sprintf("\n[Уверенность: %d%%]", searchResult.Confidence)
    *context += "\n\nИНСТРУКЦИИ ДЛЯ ОТВЕТА:\n"
    *context += "1. Используй ТОЛЬКО предоставленную информацию из поиска\n"
    *context += "2. Не добавляй факты из своих знаний\n"
    *context += "3. При недостатке информации - честно скажи об этом\n"
    *context += "4. Для творческих запросов (погода, новости) обработай информацию естественно\n"
    *context += "================================\n"
	// Добавляем дополнительные указания для LLM, если они есть
    if llmAdditional != "" {
        *context += fmt.Sprintf("\nДОПОЛНИТЕЛЬНЫЕ УКАЗАНИЯ ОТ ПОЛЬЗОВАТЕЛЯ: %s\n", llmAdditional)
    }    
    return searchQuery, true
}

// isDebugMode проверяет, включен ли режим отладки
func (a *Assistant) isDebugMode() bool {
    if a.commandHandler == nil || a.commandHandler.config == nil {
        return false
    }
    // Используем новый безопасный метод
    return a.commandHandler.config.GetBool("debug_mode")
}

// RunInteractive запускает интерактивный режим чата с поддержкой редактирования строки и истории
// RunInteractive запускает интерактивный режим чата
func (a *Assistant) RunInteractive() {
	fmt.Printf("🤖 Умный Ассистент v%s | Провайдер: %s | Модель: %s\n", Version, a.provider, a.model)
	fmt.Println("Для завершения сессии введите 'quit', 'exit' или 'bye'")
	fmt.Println("Используйте: \n\t@имя_файла для ссылки, \n\t@all для всех файлов, \n\t@http://... для веб-страни, \n\t$int для открытия в браузере, \n\t$diff применить частичные изменения к файлам, \n\t$cod работа с кодом программ, \n\t:help посмотреть возможности программы")
	fmt.Println("Нажмите Ctrl+C для отмены текущего запроса или Ctrl+D для завершения сессии")
	fmt.Println("\n")

    // Добавляем информацию об авто-копировании
    if a.autoCopyEnabled {
        fmt.Println("📋 Авто-копирование ответов: ВКЛЮЧЕНО (используйте :copy off для выключения)")
    } else {
        fmt.Println("📋 Авто-копирование ответов: выключено (используйте :copy on для включения)")
    }
    
    fmt.Println("Нажмите Ctrl+C для отмены текущего запроса или Ctrl+D для завершения сессии")
    fmt.Println("\n")
	
	// Настраиваем автодополнение
	commands := []string{
		":clean", ":pop", ":ctx", ":limit", ":summarize",
		":save", ":load", ":ls", ":rm", ":export", ":sh",
		":clip", ":clip+", ":cd", ":pwd", ":open", ":dir",
		":debug", ":stats", ":retry", ":models", ":model", ":providers", ":provider",
		":set", ":get", ":reset", ":quit", ":help", ":history", ":skip", ":data",
		":copi",
	}
	a.terminalReader.SetCompleter(commands)

	defer a.terminalReader.Close()

	// Настраиваем перехват сигналов Ctrl+C
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
    defer func() {
    	signal.Stop(sigChan)
    	close(sigChan)       
    }()
    
    // Горутина для обработки сигналов - ТОЛЬКО отмена, без выхода
    go func() {
    	for {
    		select {
    		case _, ok := <-sigChan:
    			if !ok {
    				return      
    			}
    			a.requestMu.Lock()
    			if a.requestCancel != nil {
    				fmt.Println("\n🤖 Ассистент: Отмена запроса (Ctrl+C)...")
    				a.requestCancel()
    				a.requestCancel = nil
    			} else {
    				fmt.Println("\n🤖 Ассистент: Нет активного запроса для отмены")
    			}
    			a.requestMu.Unlock()
    		}
    	}
    }()
	for {
		query, err := a.terminalReader.ReadLine()
        if err != nil {
            if err == liner.ErrPromptAborted {
                // Ctrl+C во время ввода - просто продолжаем
                fmt.Println()
                continue
            }
            // Проверяем тип ошибки через строковое представление
            if strings.Contains(err.Error(), "not a terminal") {
                fmt.Printf("❌ Ошибка: не удалось получить доступ к терминалу (возможно, не TTY)\n")
                break
            }
            if err == io.EOF {
                // Ctrl+D - выход из программы
                fmt.Println("\n🤖 Ассистент: До свидания!")
                break
            }
            fmt.Printf("❌ Ошибка ввода: %v\n", err)
            continue
        }
   
    	query = strings.TrimSpace(query)
    	if query == "" {
    		continue
    	}
    
    	// Проверка на выход
    	if isExitCommand(query) {
    		fmt.Println("\n🤖 Ассистент: До свидания!")
    		break
    	}
    
    	a.ProcessQuery(query, false)
    }
}



// sendWithStats отправляет запрос к LLM с записью статистики
func (a *Assistant) sendWithStats(status status.Context, message, provider, model, apiKey string, reqType string) (string, error) {
    startTime := time.Now()
    result, err := SendMessageToLLM(status, message, provider, model, apiKey)
    
    // Записываем статистику, если доступна
    if a.commandHandler != nil && a.commandHandler.stats != nil {
        a.commandHandler.stats.RecordRequest(time.Since(startTime), reqType)
    }
    
    return result, err
}

// isCodeCommand определяет, является ли запрос командой работы с кодом ($cod, $diff)
func (a *Assistant) isCodeCommand(query string) bool {
    lowerQuery := strings.ToLower(query)
    return strings.Contains(lowerQuery, "$cod") || 
           strings.Contains(lowerQuery, "$diff") ||
           strings.Contains(lowerQuery, "$patch")
}

// ProcessQuery обрабатывает один запрос пользователя с возможностью отмены
func (a *Assistant) ProcessQuery(query string, autoMode bool) {
    // Проверяем, является ли запрос код-командой
    isCodeCmd := a.isCodeCommand(query)

	if !strings.HasPrefix(query, ":retry") {
		a.lastUserQuery = query
	}

	// Проверяем команды
	if strings.HasPrefix(query, ":") {
		a.commandHandler.Handle(query)
		return
	}

	fmt.Println("\n🤖 Думаю...")
	// Создаем отменяемый контекст для запроса
    a.requestMu.Lock()
    if a.requestCancel != nil {
    	// Если есть старый контекст, отменяем его перед созданием нового
    	a.requestCancel()
    }
    a.requestCtx, a.requestCancel = ctx.WithCancel(ctx.Background())
    a.requestMu.Unlock()	


	// Отложенная очистка контекста после завершения
    defer func() {
        a.requestMu.Lock()
        if a.requestCancel != nil {
            a.requestCancel()
            a.requestCancel = nil
        }
        a.requestMu.Unlock()
    }()

	if a.diffProcessor.HasDiffMarker(query) {
		a.handleDiffRequest(query, autoMode)
		return
	}
	
	// Проверка на интернет-запрос
	if strings.Contains(query, "$internet") || strings.Contains(query, "$int") {
		a.handleInternetRequest(query, autoMode)
		return
	}

	refs, hasRefs := a.fileParser.ExtractFileReferences(query)
    isTextRequest := a.isTextFileRequest(refs)
    
    // Собираем контекст
    context := a.buildContext(refs, hasRefs)

   // ДОБАВЛЯЕМ RAG-КОНТЕКСТ если включен
    if a.IsRAGEnabled() {
        ragContext := a.GetRAGContext()
        if ragContext != "" {
            context += ragContext
            if a.isDebugMode() {
                fmt.Printf("🔍 RAG-режим активен (%d документов)\n", len(a.ragData))
            }
        }
    }

	// ОБРАБОТКА ЯВНОГО ЗАПРОСА ПОИСКА В ИНТЕРНЕТЕ
	var explicitSearchDone bool
	if !autoMode {
		query, explicitSearchDone = a.handleExplicitInternetSearch(query, &context)
	}
	
	// Определяем, нужен ли поиск в интернете
	if a.webSearchEnabled && !autoMode && !explicitSearchDone {
		needSearch, reason := ShouldSearch(query, a.detectLanguageFromQuery(query))
		if needSearch {
			LogSearchRequest(query, reason)
			searchResult, err := FetchTopText(a.requestCtx, query)
			if err != nil {
				fmt.Printf("⚠️ Поиск не удался: %v\n", err)
			} else {
				context += "\nИнформация из интернета:\n" + searchResult.Summary
				context += "\nИсточники: " + a.formatSources(searchResult.Sources)
				context += "\nИспользуй эту информацию для ответа, но при этом не придумывай ничего самостоятельно.\n"
			}
		}
	}

	// Формируем финальный промпт
	prompt := a.constructPrompt(query, context, isTextRequest)
	
	// Отправляем в LLM с контекстом отмены
	response, err := a.sendWithStats(a.requestCtx, prompt, a.provider, a.model, a.apiKey, "llm")
	// response, err := SendMessageToLLM(a.requestCtx, prompt, a.provider, a.model, a.apiKey)

	// Проверяем, была ли отмена запроса
    select {
    case <-a.requestCtx.Done():
    	fmt.Println("🤖 Запрос отменён пользователем")
    	return
    default:
    }
	if err != nil {
        // Проверяем, была ли отмена запроса
        if errors.Is(err, ctx.Canceled) {
            fmt.Println("🤖 Запрос отменен пользователем")
            return
        }
        fmt.Printf("❌ Ошибка LLM: %v\n", err)
        return
    }
    
	// Обрабатываем ответ
    a.handleResponseWithCommandType(response, autoMode, isTextRequest, isCodeCmd)
	// a.handleResponse(response, autoMode, isTextRequest)
	
	// Обновляем контекст беседы
	a.context.AddExchange(query, response)
}

// handleResponseWithCommandType обрабатывает ответ с учетом типа команды
func (a *Assistant) handleResponseWithCommandType(response string, autoMode bool, isTextRequest bool, isCodeCommand bool) {
    fmt.Println("\n🤖 Ассистент:\n")

    // 🆕 Проверяем, это кодогенерация или обычный ответ
    files := a.codeParser.ParseCodeBlocks(response)
    
    if len(files) > 0 {
        // Это кодогенерация - работаем как раньше
        a.processCodeGeneration(files, autoMode, isTextRequest)
    } else {
        // 🆕 Обычный ответ - проверяем на Markdown
        if IsMarkdownContent(response) {
            rendered, err := RenderMarkdown(response)
            if err == nil {
                fmt.Println(rendered)
            } else {
                // Fallback при ошибке
                fmt.Println(response)
            }
        } else {
            // Простой текст без форматирования
            fmt.Println(response)
        }
        
        if a.autoCopyEnabled && !isCodeCommand && response != "" {
            a.copyToClipboardSafely(response)
        }
    }
}

func (a *Assistant) handleDiffRequest(query string, autoMode bool) {
	cleanQuery := strings.ReplaceAll(strings.ReplaceAll(query, "$diff", ""), "$patch", "")
	cleanQuery = strings.TrimSpace(cleanQuery)
	
	files := a.diffProcessor.GetTargetFiles(query)
	if len(files) == 0 {
		fmt.Println("❌ Для $diff укажите файлы: @filename")
		return
	}
	
	fmt.Printf("🔧 Режим DIFF для: %v\n", files)
	context := a.buildDiffContext(files)
	prompt := a.constructDiffPrompt(cleanQuery, context, files)
	
	response, err := a.sendWithStats(a.requestCtx, prompt, a.provider, a.model, a.apiKey, "diff")
	// response, err := SendMessageToLLM(a.requestCtx, prompt, a.provider, a.model, a.apiKey)
	if err != nil {
		fmt.Printf("❌ Ошибка: %v\n", err)
		return
	}
	
	a.handleDiffResponse(response, autoMode)
	a.context.AddExchange(query, response)
}

func (a *Assistant) buildDiffContext(files []string) string {
	var context strings.Builder
	for _, file := range files {
		ref := FileReference{Path: file}
		context.WriteString(a.fileParser.readSingleFile(ref))
	}
	return context.String()
}

func (a *Assistant) constructDiffPrompt(query, context string, files []string) string {
	return fmt.Sprintf(`Вы - старший программист. ВНЕСИ ИЗМЕНЕНИЯ ТОЛЬКО В УКАЗАННЫЕ ФАЙЛЫ используя DIFF-формат.

ПРАВИЛА:
1. НЕ переписывай весь файл.
2. Укажи ТОЧНЫЕ строки для замены.
3. Формат:
--- Diff: path/to/file ---
Original lines X-Y:
<3 строки контекста ДО>
<оригинальные строки, которые надо заменить>
<3 строки контекста ПОСЛЕ>
Modified:
<3 строки контекста ДО>
<новые строки с теми же отступами>
<3 строки контекста ПОСЛЕ>
4. Всегда включай **ровно 3 строки до и 3 строки после** изменяемого фрагмента (если они есть).
5. Номера строк могут быть неправильными — программа найдёт блок по контексту.
6. Сохраняй отступы 1-в-1 (копируй пробелы/табы из оригинала).
7. Для удаления: оставь блок Modified пустым.
8. Несколько файлов — несколько блоков подряд.
9. В ответе **только** набор таких блоков, без пояснений.
10. ВАЖНО: НИКОГДА НЕ ВСТАВЛЯЙТЕ МАРКЕРЫ '--- Diff:' ВНУТРЬ КОДА.
   ИСПОЛЬЗУЙТЕ ИХ ТОЛЬКО ДЛЯ ОБОЗНАЧЕНИЯ ГРАНИЦ ФАЙЛОВ.
11. **НИКОГДА НЕ ОСТАВЛЯЙ ПУСТОЙ БЛОК Original** — это приведет к ошибке.

ПРИМЕРЫ:
--- Diff: main.go ---
Original lines 12-14:
    fmt.Println("hello")
    x := 1
    y := 2
Modified:
    log.Println("hello")
    x := 42
    y := 3

--- Diff: utils.go ---
Original lines 8-8:
    import "fmt"
Modified:
    import (
        "fmt"
        "log"
    )

КОД:
%s

ЗАДАЧА: %s

ВЕРНИ ТОЛЬКО DIFF-БЛОКИ.`, context, query)
}

 
func (a *Assistant) handleDiffResponse(response string, autoMode bool) {
    fmt.Println("\n🤖 Анализ изменений...")
    blocks := a.diffProcessor.ParseDiffBlocks(response)
    
    if len(blocks) == 0 {
        fmt.Println("❌ DIFF-блоки не найдены в ответе LLM")
        if a.isDebugMode() {
            fmt.Printf("Ответ LLM:\n%s\n", response)
        }
        return
    }
    
    // Группируем по файлам для понятного вывода
    fileGroups := make(map[string]int)
    compileInfoMap := make(map[string]*CompileInfo)
    
    for _, b := range blocks {
        fileGroups[b.FilePath]++
        if b.Compile != nil {
            compileInfoMap[b.FilePath] = b.Compile
        }
    }
    
    fmt.Printf("📋 Найдено %d патчей в %d файлах:\n", len(blocks), len(fileGroups))
    for file, count := range fileGroups {
        fmt.Printf("  - %s (%d изменений)\n", file, count)
        if compileInfo, ok := compileInfoMap[file]; ok {
            if compileInfo.Command != "" {
                fmt.Printf("    🔧 Компиляция: %s\n", compileInfo.Command)
            } else if compileInfo.Flags != "" {
                fmt.Printf("    🔧 Флаги: %s\n", compileInfo.Flags)
            }
        }
    }
    
    // Запрашиваем подтверждение только если не в autoMode
    if !autoMode {
        // Первоначальный запрос - без переноса строки в начале
        input, err := a.terminalReader.ReadLineWithPrompt("Применить изменения? (y/n/d - детали): ")
        if err != nil {
            fmt.Printf("❌ Ошибка ввода: %v\n", err)
            return
        }
        
        input = strings.ToLower(strings.TrimSpace(input))
        if input == "d" || input == "д" {
            // Показать детали патчей
            fmt.Println("\n🔍 Детали патчей:")
            for i, b := range blocks {
                fmt.Printf("\n%d. Файл: %s (строки %d-%d)\n", i+1, b.FilePath, b.LineStart, b.LineEnd)
                if len(b.Original) > 0 {
                    fmt.Printf("   Заменяется (%d строк):\n", len(b.Original))
                    for j, line := range b.Original {
                        if j < 3 || j >= len(b.Original)-3 { // Показываем начало и конец
                            fmt.Printf("   - %s\n", strings.TrimSpace(line))
                        } else if j == 3 {
                            fmt.Printf("   - ... (%d строк пропущено) ...\n", len(b.Original)-6)
                        }
                    }
                }
                if len(b.Modified) > 0 {
                    fmt.Printf("   На (%d строк):\n", len(b.Modified))
                    for j, line := range b.Modified {
                        if j < 3 || j >= len(b.Modified)-3 {
                            fmt.Printf("   + %s\n", strings.TrimSpace(line))
                        } else if j == 3 {
                            fmt.Printf("   + ... (%d строк пропущено) ...\n", len(b.Modified)-6)
                        }
                    }
                }
            }
            
            // Повторный запрос подтверждения после деталей - БЕЗ переноса строки
            input, err = a.terminalReader.ReadLineWithPrompt("Применить изменения? (y/n): ")
            if err != nil {
                fmt.Printf("❌ Ошибка ввода: %v\n", err)
                return
            }
            input = strings.ToLower(strings.TrimSpace(input))
        }
        
        if input != "y" && input != "у" { // Поддержка русской 'у'
            fmt.Println("❌ Отменено")
            return
        }
    }
    
    // Проверяем, не отменён ли запрос
    select {
    case <-a.requestCtx.Done():
        fmt.Println("🤖 Запрос отменён пользователем")
        return
    default:
    }

    // ПРИМЕНЯЕМ патчи с новой логикой частичного применения
    fmt.Println("\n🔧 Применение патчей...")
    if err := a.diffProcessor.ApplyDiffBlocks(blocks, autoMode); err != nil {
        // Даже при ошибках некоторые патчи могли быть применены
        fmt.Printf("⚠️  Частичные ошибки: %v\n", err)
        // Продолжаем проверку тех файлов, которые были изменены
    }
    
    // 🔍 Проверяем ВСЕ измененные файлы на ошибки (даже если были ошибки применения)
    fmt.Println("\n🔍 Проверка измененного кода...")
    for filePath := range fileGroups {
        // Проверяем, существует ли файл после изменений
        if _, err := os.Stat(filePath); os.IsNotExist(err) {
            fmt.Printf("⚠️  Файл %s не найден, пропускаем проверку\n", filePath)
            continue
        }
        
        fmt.Printf("\nПроверка файла: %s\n", filePath)
        
        // Получаем compileInfo для файла
        var compileInfo *CompileInfo
        if ci, ok := compileInfoMap[filePath]; ok {
            compileInfo = ci
        }
        
        // Вызываем специальный метод для DIFF-режима
        if err := a.codeRunner.RunDiffWithRetry(a.requestCtx, filePath, a.provider, a.model, a.apiKey, a.diffProcessor, compileInfo); err != nil {
            fmt.Printf("⚠️  Файл %s содержит ошибки: %v\n", filePath, err)
            // НЕ останавливаем проверку других файлов
            continue
        }
        
        fmt.Printf("✅ Файл %s проверен успешно\n", filePath)
    }
    
    fmt.Println("\n📊 Итог: изменения применены с частичной обработкой ошибок")
}

// buildContext собирает контекст из файлов
func (a *Assistant) buildContext(refs []FileReference, hasRefs bool) string {
		// Быстрая проверка на чрезмерный контекст
	if a.context.totalSize > MaxTotalSize {
		fmt.Printf("⚠️  Контекст достиг критического размера (%d символов), очистка...\n", a.context.totalSize)
		a.context.enforceTotalSizeLimit()
	}

	context := a.context.GetContext()

	if hasRefs {
		// РАЗДЕЛЯЕМ файлы и URL
		var fileRefs, urlRefs []FileReference
		for _, ref := range refs {
			if ref.IsURL {
				urlRefs = append(urlRefs, ref)
			} else {
				fileRefs = append(fileRefs, ref)
			}
		}
		
		// Обрабатываем файлы (существующая логика)
		if len(fileRefs) > 0 {
			fileContext := a.fileParser.ReadReferencedFiles(fileRefs)
			context += "\n" + fileContext
		}
		
		// ОБРАБАТЫВАЕМ URL
		for _, ref := range urlRefs {
			fmt.Printf("🌐 Загрузка: %s\n", ref.Path)
			urlContent, err := a.fileParser.FetchURLContent(ref.Path)
			if err != nil {
				fmt.Printf("⚠️ Не удалось загрузить URL %s: %v\n", ref.Path, err)
				continue
			}
			context += fmt.Sprintf("\n--- URL: %s ---\n%s\n", ref.Path, urlContent)
			fmt.Printf("✅ Загружено: %d символов\n", len(urlContent))
		}
	}

	return context
}


// isTextFileRequest определяет, является ли запрос текстовым (а не кодом) по наличию .txt файлов
func (a *Assistant) isTextFileRequest(refs []FileReference) bool {
    for _, ref := range refs {
        if ref.IsURL {
            return true // URL считается текстовым контентом
        }
        if strings.HasSuffix(strings.ToLower(ref.Path), ".txt") {
            return true
        }
    }
    return false
}

// constructPrompt формирует финальный промпт для LLM
// func (a *Assistant) constructPrompt(query, context string) string {
func (a *Assistant) constructPrompt(query, context string, isTextRequest bool) string {
	prompt := "Вы - старший программист и технический эксперт. "

	if context != "" {
		prompt += "Используйте следующий контекст для ответа:\n" + context + "\n\n"
	}

	// Добавляем инструкцию для Markdown только для обычных бесед
	if !a.isCodeGenerationRequest(query) && !isTextRequest && !strings.Contains(query, "$diff") {
		prompt += "ФОРМАТ ОТВЕТА: Используйте Markdown для форматирования (заголовки, жирный текст, списки, `код`). НЕ используйте --- File: --- формат.\n\n"
	}


	prompt += "Отвечайте коротко и по существу, если вас не просят об ином . Запрос пользователя: " + query

	// Если в запросе есть создание/изменение кода, указываем формат
    if a.isCodeGenerationRequest(query) && !isTextRequest {
		prompt += "\n\nВАЖНО: Если вам нужно предоставить код, используйте ТОЛЬКО следующий формат:\n"
		prompt += "--- File: имя_файла ---\n"
		prompt += "// ваш код здесь без каких-либо дополнительных тегов\n"
		prompt += "ВАЖНО: НИКОГДА НЕ ВСТАВЛЯЙТЕ МАРКЕРЫ '--- File:' ВНУТРЬ КОДА. " +
          "ИСПОЛЬЗУЙТЕ ИХ ТОЛЬКО ДЛЯ ОБОЗНАЧЕНИЯ ГРАНИЦ ФАЙЛОВ.\n"
		// prompt += "--- End File ---\n\n"
		
		// Добавляем инструкции по компиляции для сложных программ
		prompt += "ДЛЯ СЛОЖНЫХ ПРОГРАММ С ВНЕШНИМИ БИБЛИОТЕКАМИ:\n"
        prompt += "Если код требует установки зависимостей, добавьте:\n"
        prompt += "--- Install: язык ---\n"
        prompt += "команда_установки_зависимостей\n"
        prompt += "Если код требует специальных флагов компиляции, добавьте:\n"
        prompt += "--- Compile: язык ---\n"
        prompt += "флаги_компиляции_или_команда\n"
		prompt += "НЕ используйте markdown (```).\n"
		prompt += "Код должен быть чистым и готовым к выполнению.\n"
		prompt += "Если нужно создать несколько файлов, повторите этот формат для каждого файла.\n\n"
		
        prompt += "ПРИМЕРЫ:\n"
        prompt += "1. Python с зависимостями:\n"
        prompt += "--- Install: python ---\n"
        prompt += "pip install requests numpy\n"
        prompt += "--- Compile: python ---\n"
        prompt += "python3 main.py\n"
        prompt += "2. C с внешними библиотеками:\n"
        prompt += "--- Install: c ---\n"
        prompt += "sudo apt-get install libssl-dev\n"
        prompt += "--- Compile: c ---\ngcc -o myapp main.c -lssl\n"

	} else if isTextRequest {
    prompt += "\n\nВАЖНО: Пользователь работает с текстовым файлом (.txt). Сохраняйте ответ в том же формате (--- File: имя_файла ---), но без попыток компиляции или выполнения."
	}

	return prompt
}


// copyToClipboardSafely копирует текст в буфер обмена с обработкой ошибок
func (a *Assistant) copyToClipboardSafely(text string) {
    // Проверяем поддержку буфера обмена в системе
    if !CheckClipboardSupport() {
        if a.isDebugMode() {
            fmt.Println("⚠️  Буфер обмена не поддерживается в этой системе")
        }
        return
    }
    
    // Ограничиваем размер текста для копирования (чтобы не копировать огромные ответы)
    maxCopySize := 100000 // 100K символов максимум
    textToCopy := text
    if len(text) > maxCopySize {
        textToCopy = text[:maxCopySize] + "\n...[ответ обрезан для буфера обмена]..."
        if a.isDebugMode() {
            fmt.Printf("📋 Ответ обрезан с %d до %d символов\n", len(text), len(textToCopy))
        }
    }
    
    // Убираем лишние пустые строки в начале и конце
    textToCopy = strings.TrimSpace(textToCopy)
    if textToCopy == "" {
        return
    }
    
    // Копируем в буфер обмена
    err := WriteClipboard(textToCopy)
    if err != nil {
        if a.isDebugMode() {
            fmt.Printf("⚠️  Не удалось скопировать в буфер обмена: %v\n", err)
        }
    } else {
        // Показываем уведомление только в debug режиме или если ответ короткий
        if a.isDebugMode() || len(textToCopy) < 500 {
            fmt.Printf("📋 Ответ скопирован в буфер обмена (%d символов)\n", len(textToCopy))
        }
    }
}


// processCodeGeneration обрабатывает генерацию кода с анализом проекта
func (a *Assistant) processCodeGeneration(files []CodeFile, autoMode bool, isTextRequest bool) {
	fmt.Println("🔧 Анализ сгенерированного кода...")
	fmt.Printf("📋 Найдено %d файлов для создания/обновления\n\n", len(files))

	// Первый проход: записываем все файлы
	fmt.Println("📥 Запись файлов...")
	for _, f := range files {
		
		fullPath := f.Path //filepath.Join(".", f.Path)
		dir := filepath.Dir(fullPath)

		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Printf("❌ Ошибка создания директории %s: %v\n", dir, err)
			continue
		}

		if err := os.WriteFile(fullPath, []byte(f.Content), 0644); err != nil {
			fmt.Printf("❌ Ошибка записи файла %s: %v\n", f.Path, err)
			continue
		}

		fmt.Printf("✅ Файл записан: %s\n", f.Path)
	}

	// Второй проход: обрабатываем информацию о компиляции
	var compileFiles []CodeFile
	for _, f := range files {
		if f.Compile != nil {
			compileFiles = append(compileFiles, f)
		}
	}
	
	if len(compileFiles) > 0 {
		fmt.Println("\n🔧 Обработка специальных флагов компиляции...")
		for _, f := range compileFiles {
			if f.Compile.Command != "" {
				fmt.Printf("  📋 %s: %s\n", f.Path, f.Compile.Command)
			} else if f.Compile.Flags != "" {
				fmt.Printf("  📋 %s: флаги '%s'\n", f.Path, f.Compile.Flags)
			}
		}
	}

	// Проверяем и устанавливаем зависимости
	if err := a.installer.CheckAndInstallDependencies(files); err != nil {
		fmt.Printf("❌ Ошибка установки зависимостей: %v\n", err)
		return
	}

	if a.GetConfig() != nil && a.GetConfig().GetBool("debug_mode") {
        fmt.Println("🔧 Зависимости обработаны")
    }

	// === НОВЫЙ ФУНКЦИОНАЛ: Анализ и запуск проекта ===
	if !autoMode && !isTextRequest {
		analyzer := NewProjectAnalyzer(files)
		projectConfig := analyzer.Analyze()

		// Показываем структуру проекта
		fmt.Printf("\n📊 Анализ проекта:\n")
		fmt.Printf("   Язык: %s\n", projectConfig.Language)
		fmt.Printf("   Файлов: %d\n", len(projectConfig.Files))
		if projectConfig.EntryPoint != "" {
			fmt.Printf("   Точка входа: %s\n", projectConfig.EntryPoint)
		}
		if projectConfig.CompileCommand != "" {
			fmt.Printf("   Компиляция: %s\n", projectConfig.CompileCommand)
		}
		if projectConfig.RunCommand != "" {
			fmt.Printf("   Запуск: %s\n", projectConfig.RunCommand)
		}

		// Запрашиваем выбор точки входа, если есть альтернативы
		availableEntryPoints := analyzer.GetAvailableEntryPoints()
		selectedEntryPoint := projectConfig.EntryPoint

		if len(availableEntryPoints) > 1 {
			fmt.Printf("\n📋 Доступные точки входа:\n")
			for i, ep := range availableEntryPoints {
				fmt.Printf("  %d. %s\n", i+1, ep)
			}

			response, err := a.terminalReader.ReadLineWithPrompt(
				fmt.Sprintf("Выберите точку входа (1-%d, Enter для '%s'): ", 
					len(availableEntryPoints), projectConfig.EntryPoint))
			if err != nil {
				fmt.Printf("⚠️ Ошибка ввода: %v, использовано значение по умолчанию\n", err)
			} else if strings.TrimSpace(response) != "" {
				var choice int
				if _, err := fmt.Sscanf(response, "%d", &choice); err == nil && 
				   choice >= 1 && choice <= len(availableEntryPoints) {
					selectedEntryPoint = availableEntryPoints[choice-1]
				}
			}
		}

		// Запрашиваем аргументы командной строки
		var runArgs []string
		if projectConfig.RunCommand != "" {
			response, err := a.terminalReader.ReadLineWithPrompt("Аргументы командной строки (опционально): ")
			if err == nil {
				args := strings.TrimSpace(response)
				if args != "" {
					runArgs = strings.Fields(args)
				}
			}
		}

		// Обновляем конфиг
		projectConfig.EntryPoint = selectedEntryPoint
		projectConfig.Args = runArgs

		// Запускаем проект
		fmt.Printf("\n🚀 Запуск проекта...\n")
		if err := a.codeRunner.RunProject(a.requestCtx, projectConfig, a.provider, a.model, a.apiKey); err != nil {
			fmt.Printf("❌ Ошибка запуска проекта: %v\n", err)
		}
	}

	fmt.Printf("\n📊 Итог генерации: %d файлов записано\n", len(files))
}

// formatSources форматирует список источников для вывода
func (a *Assistant) formatSources(sources []Link) string {
	if len(sources) == 0 {
		return "нет источников"
	}

	var sb strings.Builder
	for i, src := range sources {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(src.Title)
	}
	return sb.String()
}

// isCodeGenerationRequest определяет, является ли запрос запросом на генерацию кода
func (a *Assistant) isCodeGenerationRequest(query string) bool {

	// keywords := []string{"напиши код", "напиши программу", "создай файл", "перепиши код", "измени код", "добавь функцию", "реализуй"}

	keywords := []string{"$cod", "$diff"}

	queryLower := strings.ToLower(query)
	for _, kw := range keywords {
		if strings.Contains(queryLower, kw) {
			return true
		}
	}
	return false
}

// handleInternetRequest обрабатывает запросы с маркером $internet/$int
func (a *Assistant) handleInternetRequest(query string, autoMode bool) {
	// Удаляем маркеры из запроса
	cleanQuery := strings.ReplaceAll(strings.ReplaceAll(query, "$internet", ""), "$int", "")
	cleanQuery = strings.TrimSpace(cleanQuery)
	
	if cleanQuery == "" {
		fmt.Println("❌ Пустой запрос после удаления маркера $internet")
		return
	}
	
	fmt.Println("\n🌐 Формирую URL для запроса...")
	
	// Промпт для генерации URL — максимально строгий, без explanation
	prompt := fmt.Sprintf(`Сгенерируй правильный URL для следующего запроса пользователя. 
Верни ТОЛЬКО URL, без какого-либо дополнительного текста, пояснений или markdown.

Запрос пользователя: "%s"

Примеры:
Запрос: "Открой сайт газеты Вашингтон пост" -> https://www.washingtonpost.com
Запрос: "Найди в Google информацию про Дональда Трампа" -> https://www.google.com/search?q=Donald+Trump
Запрос: "GitHub" -> https://github.com

URL:`, cleanQuery)
	
	// Отправляем запрос в LLM
	response, err := a.sendWithStats(ctx.Background(), prompt, a.provider, a.model, a.apiKey, "internet")
	// response, err := SendMessageToLLM(ctx.Background(), prompt, a.provider, a.model, a.apiKey)
	if err != nil {
		fmt.Printf("❌ Ошибка при формировании URL: %v\n", err)
		return
	}
	
	// Очищаем ответ от возможного markdown и пробелов
	url := strings.TrimSpace(response)
	url = strings.TrimPrefix(url, "```")
	url = strings.TrimSuffix(url, "```")
	url = strings.TrimSpace(url)
	
	// Валидация URL
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		fmt.Printf("❌ Полученный ответ не является валидным URL: %s\n", url)
		fmt.Printf("🤖 Ответ LLM: %s\n", response)
		return
	}
	
	fmt.Printf("✅ Сформирован URL: %s\n", url)
	
    // В интерактивном режиме запрашиваем подтверждение
    if !autoMode {
    	response, err := a.terminalReader.ReadLineWithPrompt("Открыть ссылку в браузере? (y/n): ")
    	if err != nil {
    		if err == liner.ErrPromptAborted {
    			fmt.Println("❌ Операция отменена")
    			return
    		}
    		fmt.Printf("❌ Ошибка ввода: %v\n", err)
    		return
    	}
    	if strings.ToLower(strings.TrimSpace(response)) != "y" {
    		fmt.Println("❌ Операция отменена пользователем")
    		return
    	}
    }
    
    // Проверяем, не отменён ли запрос во время ожидания ввода
    select {
    case <-a.requestCtx.Done():
    	fmt.Println("🤖 Запрос отменён пользователем")
    	return
    default:
    }

	// Открываем URL
	fmt.Printf("🚀 Открываю браузер...\n")
	if err := OpenURLInBrowser(url); err != nil {
		fmt.Printf("❌ Ошибка открытия браузера: %v\n", err)
		return
	}
	
	fmt.Printf("✅ Браузер открыт\n")
	
	// Обновляем контекст беседы
	a.context.AddExchange(query, fmt.Sprintf("Открыт URL: %s", url))
}

// detectLanguageFromQuery определяет язык программирования из запроса
func (a *Assistant) detectLanguageFromQuery(query string) string {
	langMap := map[string][]string{
		"go":         {"go", "golang"},
		"python":     {"python", "python3", "py"},
		"cpp":        {"cpp", "c++", "cplusplus"},
		"c":          {"c язык", "на c "},
		"fortran":    {"fortran", "f90", "f95"},
		"ruby":       {"ruby", "rb"},
		"kotlin":     {"kotlin", "kt"},
		"swift":      {"swift"},
		"html":       {"html"},
		"assembly":   {"assembly", "asm"},
		"lisp":       {"lisp", "cl"},
	}

	queryLower := strings.ToLower(query)
	for lang, keywords := range langMap {
		for _, kw := range keywords {
			if strings.Contains(queryLower, kw) {
				return lang
			}
		}
	}
	return ""
}

// isExitCommand проверяет, является ли команда командой выхода
func isExitCommand(cmd string) bool {
	exitCommands := []string{"quit", "exit", "bye", "выход"}
	cmdLower := strings.ToLower(strings.TrimSpace(cmd))
	for _, ec := range exitCommands {
		if cmdLower == ec {
			return true
		}
	}
	return false
}
