// server.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
    "runtime"
	"sort"
	"net/http"
	"strings"
	"sync"
	"time"
	"os/exec"
	"path/filepath"
	"strconv"
	"net"
	"io"

	"github.com/gorilla/websocket"
)

// WebServer представляет HTTP-сервер для веб-интерфейса
type WebServer struct {
	assistant   *Assistant
	upgrader    websocket.Upgrader
	connections map[*websocket.Conn]bool
	mu          sync.RWMutex
	port        string
	config      *Config
	fsManager   *FileSystemManager
}

// NewWebServer создает новый веб-сервер
func NewWebServer(assistant *Assistant, port string) *WebServer {
	config := NewConfig()
	config.Load()

	return &WebServer{
		assistant: assistant,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true // В продакшене нужно ограничить домены
			},
		},
		connections: make(map[*websocket.Conn]bool),
		port:        port,
		config:      config,
	 	fsManager:   NewFileSystemManager(), 
	}
}

// WSMessage представляет сообщение WebSocket
type WSMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

func (ws *WebServer) StartWithListener(addr string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	
	configDir := filepath.Join(home, ".cogitor/web")
    // Статические файлы
    http.Handle("/", http.FileServer(http.Dir(configDir)))
    
	// API endpoint
	http.HandleFunc("/api/ws", ws.handleWebSocket)
    http.HandleFunc("/api/context/limit", ws.handleSetContextLimit)
	http.HandleFunc("/api/status", ws.handleStatus)
	http.HandleFunc("/api/command", ws.handleCommand)
	http.HandleFunc("/api/config", ws.handleConfig)
	http.HandleFunc("/api/sessions", ws.handleSessions)
    http.HandleFunc("/api/rag/upload", ws.handleRAGUpload)
    http.HandleFunc("/api/rag/enable", ws.handleRAGEnable)
    http.HandleFunc("/api/rag/disable", ws.handleRAGDisable)
    http.HandleFunc("/api/rag/status", ws.handleRAGStatus)
    http.HandleFunc("/api/sessions/save", ws.handleSessionsSave)
    http.HandleFunc("/api/sessions/load", ws.handleSessionsLoad)
    http.HandleFunc("/api/sessions/list", ws.handleSessionsList)
	http.HandleFunc("/api/system/info", ws.handleSystemInfo)
    http.HandleFunc("/api/provider/change", ws.handleProviderChange)
	http.HandleFunc("/api/sessions/delete", ws.handleSessionsDelete)
    
    listener, err := net.Listen("tcp", addr)
    if err != nil {
        return err
    }
    
    log.Printf("🚀 Веб-сервер запущен на http://%s", listener.Addr().String())
    log.Printf("📁 Статические файлы: ./web/")
    log.Printf("🔗 WebSocket endpoint: ws://%s/api/ws", listener.Addr().String())
    
    return http.Serve(listener, nil)
}

// Start запускает веб-сервер
func (ws *WebServer) Start() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	
	configDir := filepath.Join(home, ".cogitor/web")
	// Статические файлы
	http.Handle("/", http.FileServer(http.Dir(configDir)))
	
	// API endpoint
	http.HandleFunc("/api/ws", ws.handleWebSocket)
    http.HandleFunc("/api/context/limit", ws.handleSetContextLimit)
	http.HandleFunc("/api/status", ws.handleStatus)
	http.HandleFunc("/api/command", ws.handleCommand)
	http.HandleFunc("/api/config", ws.handleConfig)
	http.HandleFunc("/api/sessions", ws.handleSessions)
    http.HandleFunc("/api/rag/upload", ws.handleRAGUpload)
    http.HandleFunc("/api/rag/enable", ws.handleRAGEnable)
    http.HandleFunc("/api/rag/disable", ws.handleRAGDisable)
    http.HandleFunc("/api/rag/status", ws.handleRAGStatus)
    http.HandleFunc("/api/sessions/save", ws.handleSessionsSave)
    http.HandleFunc("/api/sessions/load", ws.handleSessionsLoad)
    http.HandleFunc("/api/sessions/list", ws.handleSessionsList)
	http.HandleFunc("/api/system/info", ws.handleSystemInfo)
    http.HandleFunc("/api/provider/change", ws.handleProviderChange)
	http.HandleFunc("/api/sessions/delete", ws.handleSessionsDelete)
	
	addr := fmt.Sprintf(":%s", ws.port)
	
	log.Printf("🚀 Веб-сервер запущен на http://localhost%s", addr)
	log.Printf("📁 Статические файлы: ./web/")
	log.Printf("🔗 WebSocket endpoint: ws://localhost%s/api/ws", addr)
	
	return http.ListenAndServe(addr, nil)
}

// handleRAGUpload обрабатывает загрузку файлов для RAG
func (ws *WebServer) handleRAGUpload(w http.ResponseWriter, r *http.Request) {
    if r.Method != "POST" {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }
    
    // Максимальный размер файла - 10MB
    if err := r.ParseMultipartForm(10 << 20); err != nil {
        http.Error(w, fmt.Sprintf("Файл слишком большой: %v", err), http.StatusBadRequest)
        return
    }
    
    file, handler, err := r.FormFile("file")
    if err != nil {
        http.Error(w, fmt.Sprintf("Ошибка получения файла: %v", err), http.StatusBadRequest)
        return
    }
    defer file.Close()
    
    // Читаем содержимое файла
    data, err := io.ReadAll(file)
    if err != nil {
        http.Error(w, fmt.Sprintf("Ошибка чтения файла: %v", err), http.StatusBadRequest)
        return
    }
    
    // Проверяем поддерживаемый формат
    ext := strings.ToLower(filepath.Ext(handler.Filename))
    supported := false
    for _, supportedExt := range []string{".txt", ".json", ".csv", ".md", ".xml", ".yaml", ".yml"} {
        if ext == supportedExt {
            supported = true
            break
        }
    }
    
    if !supported {
        http.Error(w, fmt.Sprintf("Неподдерживаемый формат файла: %s", ext), http.StatusBadRequest)
        return
    }
    
    // Создаем RAG документ
    ragDoc := RAGDocument{
        FilePath: handler.Filename,
        Content:  string(data),
        Size:     len(data),
        LoadedAt: time.Now(),
    }
    
    // Добавляем документ к существующим данным
    currentData := ws.assistant.GetRAGData()
    currentData = append(currentData, ragDoc)
    ws.assistant.SetRAGData(currentData)
    
    response := map[string]interface{}{
        "success": true,
        "message": fmt.Sprintf("Файл загружен: %s (%d символов)", handler.Filename, len(data)),
        "size":    len(data),
        "time":    time.Now().Format(time.RFC3339),
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}

// handleRAGEnable включает RAG режим
func (ws *WebServer) handleRAGEnable(w http.ResponseWriter, r *http.Request) {
    if r.Method != "POST" {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }
    
    var data struct {
        Enabled bool `json:"enabled"`
    }
    
    if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
        http.Error(w, "Некорректный JSON", http.StatusBadRequest)
        return
    }
    
    // Если есть загруженные данные, активируем RAG
    ragData := ws.assistant.GetRAGData()
    enabled := data.Enabled && len(ragData) > 0
    
    if enabled {
        // Рассылаем обновление статуса всем клиентам
        ws.broadcastRAGStatus()
        
        response := map[string]interface{}{
            "success":   true,
            "enabled":   true,
            "message":   fmt.Sprintf("RAG режим активирован (%d документов)", len(ragData)),
            "documents": ragData,
            "totalSize": ws.calculateRAGTotalSize(ragData),
            "time":      time.Now().Format(time.RFC3339),
        }
        
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(response)
    } else {
        response := map[string]interface{}{
            "success": false,
            "enabled": false,
            "message": "Нет загруженных данных для активации RAG",
            "time":    time.Now().Format(time.RFC3339),
        }
        
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(response)
    }
}

// handleRAGDisable отключает RAG режим
func (ws *WebServer) handleRAGDisable(w http.ResponseWriter, r *http.Request) {
    if r.Method != "POST" {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }
    
    // Очищаем RAG данные
    ws.assistant.ClearRAGData()
    
    // Рассылаем обновление статуса всем клиентам
    ws.broadcastRAGStatus()
    
    response := map[string]interface{}{
        "success": true,
        "enabled": false,
        "message": "RAG режим отключен, данные очищены",
        "time":    time.Now().Format(time.RFC3339),
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}

// handleRAGStatus возвращает статус RAG режима
func (ws *WebServer) handleRAGStatus(w http.ResponseWriter, r *http.Request) {
    if r.Method != "GET" {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }
    
    ragData := ws.assistant.GetRAGData()
    enabled := ws.assistant.IsRAGEnabled()
    
    response := map[string]interface{}{
        "success":   true,
        "enabled":   enabled,
        "documents": ragData,
        "totalSize": ws.calculateRAGTotalSize(ragData),
        "time":      time.Now().Format(time.RFC3339),
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}

// calculateRAGTotalSize вычисляет общий размер RAG документов
func (ws *WebServer) calculateRAGTotalSize(docs []RAGDocument) int {
    total := 0
    for _, doc := range docs {
        total += doc.Size
    }
    return total
}

// broadcastRAGStatus рассылает обновление статуса RAG всем клиентам
func (ws *WebServer) broadcastRAGStatus() {
    ragData := ws.assistant.GetRAGData()
    enabled := ws.assistant.IsRAGEnabled()
    
    msg := WSMessage{
        Type: "rag_status",
        Payload: map[string]interface{}{
            "enabled":   enabled,
            "documents": ragData,
            "totalSize": ws.calculateRAGTotalSize(ragData),
            "timestamp": time.Now().Format(time.RFC3339),
        },
    }
    
    ws.mu.RLock()
    defer ws.mu.RUnlock()
    
    for conn := range ws.connections {
        if err := conn.WriteJSON(msg); err != nil {
            log.Printf("❌ Ошибка отправки статуса RAG: %v", err)
            conn.Close()
            delete(ws.connections, conn)
        }
    }
}

// handleSessionsDelete обрабатывает удаление сессии через API
func (ws *WebServer) handleSessionsDelete(w http.ResponseWriter, r *http.Request) {
    if r.Method != "POST" {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }
    
    var data struct {
        Name string `json:"name"`
    }
    
    if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
        http.Error(w, "Некорректный JSON", http.StatusBadRequest)
        return
    }
    
    if data.Name == "" {
        http.Error(w, "Имя сессии не указано", http.StatusBadRequest)
        return
    }
    
    // Используем существующий CommandHandler для удаления
    ws.assistant.commandHandler.handleRemove([]string{data.Name})
    
    response := map[string]interface{}{
        "success": true,
        "message": fmt.Sprintf("Сессия удалена: %s", data.Name),
        "time":    time.Now().Format(time.RFC3339),
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}

// handleProviderChange обрабатывает смену провайдера через веб-интерфейс
func (ws *WebServer) handleProviderChange(w http.ResponseWriter, r *http.Request) {
    if r.Method != "POST" {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }
    
    var data struct {
        Provider string `json:"provider"`
        Model    string `json:"model"`
        APIKey   string `json:"api_key"`
    }
    
    if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
        http.Error(w, "Некорректный JSON", http.StatusBadRequest)
        return
    }
    
    // Валидация входных данных
    if data.Provider == "" {
        http.Error(w, "Провайдер не указан", http.StatusBadRequest)
        return
    }
    
    if data.Model == "" {
        http.Error(w, "Модель не указана", http.StatusBadRequest)
        return
    }
    
    // Проверяем, является ли провайдер URL
    if isURLLLM(data.Provider) {
        // URL-провайдер - валидируем URL
        if !strings.HasPrefix(data.Provider, "http://") && !strings.HasPrefix(data.Provider, "https://") {
            http.Error(w, "Некорректный URL провайдера", http.StatusBadRequest)
            return
        }
    } else {
        // Именованный провайдер - проверяем поддержку
        if !IsSupportedProvider(data.Provider) {
            // Если это не стандартный провайдер и не URL, сообщаем об ошибке
            http.Error(w, fmt.Sprintf("Неподдерживаемый провайдер: %s", data.Provider), http.StatusBadRequest)
            return
        }
    }
    
    // Сохраняем предыдущие значения для отката
    oldProvider := ws.assistant.provider
    oldModel := ws.assistant.model
    
    // Пробная проверка соединения с новым провайдером
    testCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    testMessage := "Привет! Это тестовое сообщение для проверки соединения."
    
    // Попытка отправить тестовый запрос
    _, err := SendMessageToLLM(testCtx, testMessage, data.Provider, data.Model, data.APIKey)
    
    if err != nil {
        // Восстанавливаем старые значения
        ws.assistant.provider = oldProvider
        ws.assistant.model = oldModel
        
        response := map[string]interface{}{
            "success": false,
            "message": fmt.Sprintf("Не удалось подключиться к провайдеру: %v", err),
            "old_provider": oldProvider,
            "old_model":    oldModel,
        }
        
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(response)
        return
    }
    
    // Успешно - применяем новые значения
    ws.assistant.provider = data.Provider
    ws.assistant.model = data.Model
    if data.APIKey != "" {
        ws.assistant.apiKey = data.APIKey
    }
    
    // Рассылаем обновление всем подключенным клиентам
    ws.broadcastProviderUpdate()
    
    response := map[string]interface{}{
        "success":     true,
        "message":     "Провайдер успешно изменен",
        "provider":    data.Provider,
        "model":       data.Model,
        "api_key_set": data.APIKey != "",
        "time":        time.Now().Format(time.RFC3339),
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}

// broadcastProviderUpdate рассылает обновление информации о провайдере
func (ws *WebServer) broadcastProviderUpdate() {
    msg := WSMessage{
        Type: "provider_updated",
        Payload: map[string]interface{}{
            "provider":    ws.assistant.provider,
            "model":       ws.assistant.model,
            "api_key_set": ws.assistant.apiKey != "",
            "timestamp":   time.Now().Format(time.RFC3339),
        },
    }
    
    ws.mu.RLock()
    defer ws.mu.RUnlock()
    
    for conn := range ws.connections {
        if err := conn.WriteJSON(msg); err != nil {
            log.Printf("❌ Ошибка отправки обновления провайдера: %v", err)
            conn.Close()
            delete(ws.connections, conn)
        }
    }
}

func (ws *WebServer) handleDetailedStats(w http.ResponseWriter, r *http.Request) {
    if r.Method != "GET" {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }
    
    stats := ws.assistant.commandHandler.stats.GetStats()
    context := ws.assistant.context
    
    response := map[string]interface{}{
        "success": true,
        "stats":   stats,
        "context": map[string]interface{}{
            "exchanges":       context.GetExchangeCount(),
            "max_length":      context.GetMaxLength(),
            "estimated_tokens": context.GetEstimatedTokens(),
            "usage_percent":   float64(context.GetExchangeCount()) / float64(context.GetMaxLength()) * 100,
        },
        "system": map[string]interface{}{
            "provider": ws.assistant.provider,
            "model":    ws.assistant.model,
            "uptime":   time.Since(startTime).String(),
        },
        "time": time.Now().Format(time.RFC3339),
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}


// handleWebSocket обрабатывает WebSocket соединения
func (ws *WebServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := ws.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("❌ Ошибка WebSocket: %v", err)
		return
	}
	
	// Регистрируем соединение
	ws.mu.Lock()
	ws.connections[conn] = true
	ws.mu.Unlock()
	
	defer func() {
		ws.mu.Lock()
		delete(ws.connections, conn)
		ws.mu.Unlock()
		conn.Close()
	}()
	
	// Отправляем приветственное сообщение
	ws.sendMessage(conn, WSMessage{
		Type: "welcome",
		Payload: map[string]interface{}{
			"version":   Version,
			"provider":  ws.assistant.provider,
			"model":     ws.assistant.model,
			"timestamp": time.Now().Format(time.RFC3339),
		},
	})

    // Отправляем статус RAG при подключении
    ws.sendRAGStatus(conn)

	// Обработка сообщений
	for {
		var msg WSMessage
		err := conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("⚠️  WebSocket закрыт: %v", err)
			}
			break
		}
		
		ws.handleWSMessage(conn, msg)
	}
}

// handleWSMessage обрабатывает сообщения от клиента
func (ws *WebServer) handleWSMessage(conn *websocket.Conn, msg WSMessage) {
	switch msg.Type {
	case "query":
		ws.handleQuery(conn, msg)
	case "command":
		ws.handleCommandWS(conn, msg)
	case "context_update":
		ws.sendContext(conn)
	case "file_upload":
		ws.handleFileUpload(conn, msg)
    case "rag_status":
        ws.sendRAGStatus(conn)
    case "fs_cd":
        ws.handleFSCdWS(conn, msg)
    case "fs_ls":
        ws.handleFSLsWS(conn, msg)
    case "fs_open":
        ws.handleFSOpenWS(conn, msg)
	default:
		ws.sendError(conn, fmt.Sprintf("Неизвестный тип сообщения: %s", msg.Type))
	}
}

// sendRAGStatus отправляет статус RAG клиенту
func (ws *WebServer) sendRAGStatus(conn *websocket.Conn) {
    ragData := ws.assistant.GetRAGData()
    enabled := ws.assistant.IsRAGEnabled()

    ws.sendMessage(conn, WSMessage{
        Type: "rag_status",
        Payload: map[string]interface{}{
            "enabled":   enabled,
            "documents": ragData,
            "totalSize": ws.calculateRAGTotalSize(ragData),
            "timestamp": time.Now().Format(time.RFC3339),
        },
    })
}

// handleFSCdWS обрабатывает смену директории через WebSocket
func (ws *WebServer) handleFSCdWS(conn *websocket.Conn, msg WSMessage) {
    path, ok := msg.Payload.(string)
    if !ok {
        ws.sendError(conn, "Некорректный путь")
        return
    }
    
    if err := ws.fsManager.ChangeDir(path); err != nil {
        ws.sendError(conn, err.Error())
        return
    }
    
    ws.sendMessage(conn, WSMessage{
        Type: "fs_cd_result",
        Payload: map[string]interface{}{
            "success":     true,
            "current_dir": ws.fsManager.GetCurrentDir(),
            "message":     "Директория изменена",
        },
    })
}

// handleFSLsWS обрабатывает запрос содержимого директории
func (ws *WebServer) handleFSLsWS(conn *websocket.Conn, msg WSMessage) {
    path := ""
    if strPath, ok := msg.Payload.(string); ok {
        path = strPath
    }
    
    entries, err := ws.fsManager.ListDir(path)
    if err != nil {
        ws.sendError(conn, err.Error())
        return
    }
    
    ws.sendMessage(conn, WSMessage{
        Type: "fs_ls_result",
        Payload: map[string]interface{}{
            "success": true,
            "path":    path,
            "entries": entries,
            "count":   len(entries),
        },
    })
}

// handleFSOpenWS открывает файл в редакторе через WebSocket
func (ws *WebServer) handleFSOpenWS(conn *websocket.Conn, msg WSMessage) {
    filePath, ok := msg.Payload.(string)
    if !ok {
        ws.sendError(conn, "Некорректный путь к файлу")
        return
    }
    
    if err := ws.fsManager.OpenInEditor(filePath); err != nil {
        ws.sendError(conn, err.Error())
        return
    }
    
    ws.sendMessage(conn, WSMessage{
        Type: "fs_open_result",
        Payload: map[string]interface{}{
            "success": true,
            "message": "Файл открыт в редакторе",
            "file":    filePath,
        },
    })
}

// handleSystemInfo обрабатывает запрос системной информации
func (ws *WebServer) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
    if r.Method != "GET" {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }
    
    // Получаем количество активных соединений
    ws.mu.RLock()
    connections := len(ws.connections)
    ws.mu.RUnlock()
    
    // Получаем количество сохраненных сессий
    sessionsCount := 0
    dir := getSessionsDir()
    if files, err := os.ReadDir(dir); err == nil {
        for _, f := range files {
            if !f.IsDir() && strings.HasSuffix(f.Name(), ".json") {
                sessionsCount++
            }
        }
    }
    
    // Получаем рабочую директорию
    workingDir, _ := os.Getwd()
    
    // Получаем состояние веб-поиска из ассистента
    webSearchEnabled := ws.assistant.webSearchEnabled
    
    // Получаем режим отладки из конфига
    debugMode := ws.config.GetBool("debug_mode")
    
    response := map[string]interface{}{
        "success": true,
        "system": map[string]interface{}{
            "provider":           ws.assistant.provider,
            "model":              ws.assistant.model,
            "version":            Version,
            "uptime":             formatDuration(time.Since(startTime)),
            "start_time":         startTime.Format("2006-01-02 15:04:05"),
            "web_search_enabled": webSearchEnabled,
            "connections":        connections,
            "sessions_count":     sessionsCount,
            "go_version":         runtime.Version(),
            "working_directory":  workingDir,
            "debug_mode":         debugMode,
        },
        "time": time.Now().Format(time.RFC3339),
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}

// formatDuration форматирует продолжительность в читаемый вид
func formatDuration(d time.Duration) string {
    days := int(d.Hours() / 24)
    hours := int(d.Hours()) % 24
    minutes := int(d.Minutes()) % 60
    seconds := int(d.Seconds()) % 60
    
    parts := []string{}
    if days > 0 {
        parts = append(parts, fmt.Sprintf("%d дн", days))
    }
    if hours > 0 {
        parts = append(parts, fmt.Sprintf("%d ч", hours))
    }
    if minutes > 0 {
        parts = append(parts, fmt.Sprintf("%d мин", minutes))
    }
    if seconds > 0 || len(parts) == 0 {
        parts = append(parts, fmt.Sprintf("%d сек", seconds))
    }
    
    return strings.Join(parts, " ")
}

// handleQuery обрабатывает запросы пользователя
func (ws *WebServer) handleQuery(conn *websocket.Conn, msg WSMessage) {
	query, ok := msg.Payload.(string)
	if !ok {
		ws.sendError(conn, "Некорректный запрос")
		return
	}
	
	// Отправляем статус "думаю"
	ws.sendMessage(conn, WSMessage{
		Type: "thinking",
		Payload: map[string]interface{}{
			"query": query,
			"time":  time.Now().Format(time.RFC3339),
		},
	})
	
	// Обрабатываем запрос в отдельной горутине
	go func() {
		// Создаем контекст с таймаутом
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		
		// Устанавливаем контекст в ассистенте
		ws.assistant.requestMu.Lock()
		if ws.assistant.requestCancel != nil {
			ws.assistant.requestCancel()
		}
		ws.assistant.requestCtx, ws.assistant.requestCancel = ctx, cancel
		ws.assistant.requestMu.Unlock()
		
		defer func() {
			ws.assistant.requestMu.Lock()
			if ws.assistant.requestCancel != nil {
				ws.assistant.requestCancel()
				ws.assistant.requestCancel = nil
			}
			ws.assistant.requestMu.Unlock()
		}()
		
		// Если это команда, обрабатываем через CommandHandler
		if strings.HasPrefix(query, ":") { // ТЕПЕРЬ strings доступен
			ws.handleCommandResponse(conn, query)
			return
		}
		
		// Обычный запрос
		response, err := ws.processQuery(ctx, query)
		if err != nil {
			ws.sendError(conn, err.Error())
			return
		}
		
		// Отправляем ответ
		ws.sendMessage(conn, WSMessage{
			Type: "response",
			Payload: map[string]interface{}{
				"query":    query,
				"response": response,
				"time":     time.Now().Format(time.RFC3339),
				"markdown": IsMarkdownContent(response),
			},
		})
		
		// Обновляем контекст для всех клиентов
		ws.broadcastContext()
	}()
}

// handleSetContextLimit обрабатывает установку лимита контекста
func (ws *WebServer) handleSetContextLimit(w http.ResponseWriter, r *http.Request) {
    if r.Method != "POST" {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }
    
    var data struct {
        Limit int `json:"limit"`
    }
    
    if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
        http.Error(w, "Некорректный JSON", http.StatusBadRequest)
        return
    }
    
    // Валидация лимита
    if data.Limit <= 0 || data.Limit > 100 {
        http.Error(w, "Лимит должен быть в диапазоне 1-100", http.StatusBadRequest)
        return
    }
    
    // Устанавливаем лимит через конфиг (чтобы сохранить для будущих сессий)
    ws.config.Set("context_limit", strconv.Itoa(data.Limit))
    ws.config.Save()
    
    // Применяем лимит к текущему контексту
    if err := ws.assistant.context.SetMaxLength(data.Limit); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    
    response := map[string]interface{}{
        "success": true,
        "message": fmt.Sprintf("Лимит контекста установлен на %d", data.Limit),
        "limit":   data.Limit,
        "time":    time.Now().Format(time.RFC3339),
    }
    
    // Рассылаем обновление всем клиентам
    ws.broadcastContext()
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}

// processQuery обрабатывает запрос через существующего ассистента
func (ws *WebServer) processQuery(ctx context.Context, query string) (string, error) {
	// Используем существующую логику ассистента
	refs, hasRefs := ws.assistant.fileParser.ExtractFileReferences(query)
	contextStr := ws.assistant.buildContext(refs, hasRefs)

    ragContext := ws.assistant.GetRAGContext()
    if ragContext != "" {
        contextStr += ragContext
    }	


	// Формируем промпт
	prompt := ws.assistant.constructPrompt(query, contextStr, ws.assistant.isTextFileRequest(refs))
	
	// Отправляем в LLM
	response, err := SendMessageToLLM(ctx, prompt, 
		ws.assistant.provider, ws.assistant.model, ws.assistant.apiKey)
	if err != nil {
		return "", err
	}
	
	// Обновляем контекст беседы
	ws.assistant.context.AddExchange(query, response)
	
	return response, nil
}

// handleCommandWS обрабатывает команды через WebSocket
func (ws *WebServer) handleCommandWS(conn *websocket.Conn, msg WSMessage) {
	command, ok := msg.Payload.(string)
	if !ok {
		ws.sendError(conn, "Некорректная команда")
		return
	}
	
	ws.handleCommandResponse(conn, command)
}

// handleCommandResponse обрабатывает команду и отправляет результат
func (ws *WebServer) handleCommandResponse(conn *websocket.Conn, command string) {
	// Используем существующий CommandHandler
	ws.assistant.commandHandler.Handle(command)
	
	// Собираем результат (для простоты - фиксированный ответ)
	result := fmt.Sprintf("Команда выполнена: %s", command)
	
	ws.sendMessage(conn, WSMessage{
		Type: "command_result",
		Payload: map[string]interface{}{
			"command": command,
			"result":  result,
			"time":    time.Now().Format(time.RFC3339),
		},
	})
	
	// Обновляем контекст
	ws.sendContext(conn)
}

// handleFileUpload обрабатывает загрузку файлов
func (ws *WebServer) handleFileUpload(conn *websocket.Conn, msg WSMessage) {
	// TODO: Реализовать загрузку файлов
	ws.sendMessage(conn, WSMessage{
		Type: "file_upload_result",
		Payload: map[string]interface{}{
			"success": true,
			"message": "Загрузка файлов будет реализована в следующей версии",
		},
	})
}

// sendContext отправляет текущий контекст клиенту
func (ws *WebServer) sendContext(conn *websocket.Conn) {
	context := ws.assistant.context.GetContext()
	exchanges := ws.assistant.context.GetAllExchanges()
	
	ws.sendMessage(conn, WSMessage{
		Type: "context",
		Payload: map[string]interface{}{
			"exchanges":     exchanges,
			"count":         len(exchanges),
			"max_length":    ws.assistant.context.GetMaxLength(),
			"estimated_tokens": ws.assistant.context.GetEstimatedTokens(),
			"raw":           context,
		},
	})
}

// broadcastContext рассылает обновление контекста всем подключенным клиентам
func (ws *WebServer) broadcastContext() {
	context := ws.assistant.context.GetContext()
	exchanges := ws.assistant.context.GetAllExchanges()
	
	msg := WSMessage{
		Type: "context_updated",
		Payload: map[string]interface{}{
			"exchanges":     exchanges,
			"count":         len(exchanges),
			"max_length":    ws.assistant.context.GetMaxLength(),
			"estimated_tokens": ws.assistant.context.GetEstimatedTokens(),
			"raw":           context, 
		},
	}
	
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	
	for conn := range ws.connections {
		if err := conn.WriteJSON(msg); err != nil {
			log.Printf("❌ Ошибка отправки контекста: %v", err)
			conn.Close()
			delete(ws.connections, conn)
		}
	}
}

// sendMessage отправляет сообщение клиенту
func (ws *WebServer) sendMessage(conn *websocket.Conn, msg WSMessage) {
	if err := conn.WriteJSON(msg); err != nil {
		log.Printf("❌ Ошибка отправки сообщения: %v", err)
	}
}

// sendError отправляет сообщение об ошибке
func (ws *WebServer) sendError(conn *websocket.Conn, errorMsg string) {
	ws.sendMessage(conn, WSMessage{
		Type: "error",
		Payload: map[string]interface{}{
			"message": errorMsg,
			"time":    time.Now().Format(time.RFC3339),
		},
	})
}

// Обработчики HTTP API

func (ws *WebServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"version":   Version,
		"provider":  ws.assistant.provider,
		"model":     ws.assistant.model,
		"status":    "running",
		"uptime":    time.Since(startTime).String(),
		"timestamp": time.Now().Format(time.RFC3339),
		"context": map[string]interface{}{
			"exchanges":     ws.assistant.context.GetExchangeCount(),
			"max_length":    ws.assistant.context.GetMaxLength(),
			"estimated_tokens": ws.assistant.context.GetEstimatedTokens(),
		},
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (ws *WebServer) handleCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}
	
	var data struct {
		Command string `json:"command"`
	}
	
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Некорректный JSON", http.StatusBadRequest)
		return
	}
	
	// Выполняем команду
	ws.assistant.commandHandler.Handle(data.Command)
	
	response := map[string]interface{}{
		"success": true,
		"command": data.Command,
		"time":    time.Now().Format(time.RFC3339),
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (ws *WebServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		config := map[string]interface{}{
			"provider":  ws.assistant.provider,
			"model":     ws.assistant.model,
			"settings":  ws.config.GetAll(),
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(config)
		
	case "POST":
		var data map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Некорректный JSON", http.StatusBadRequest)
			return
		}
		
		// Обновляем настройки
		for key, value := range data {
			if strVal, ok := value.(string); ok {
				ws.config.Set(key, strVal)
			}
		}
		
		ws.config.Save()
		
		response := map[string]interface{}{
			"success": true,
			"message": "Конфигурация обновлена",
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		
	default:
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
	}
}

// FileSystemManager управляет файловой системой для веб-интерфейса
type FileSystemManager struct {
    currentDir string
    allowedDirs []string
    mu          sync.RWMutex
}

// NewFileSystemManager создает менеджер файловой системы
func NewFileSystemManager() *FileSystemManager {
    dir, _ := os.Getwd()
    home, _ := os.UserHomeDir()
    
    return &FileSystemManager{
        currentDir: dir,
        allowedDirs: []string{
            dir,
            home,
            "/tmp",
            "/var/tmp",
        },
    }
}

// ChangeDir безопасно меняет рабочую директорию
func (fsm *FileSystemManager) ChangeDir(path string) error {
    fsm.mu.Lock()
    defer fsm.mu.Unlock()
    
    // Разрешаем относительные пути
    if !filepath.IsAbs(path) {
        path = filepath.Join(fsm.currentDir, path)
    }
    
    // Очищаем путь
    cleanPath := filepath.Clean(path)
    
    // Проверяем существование
    info, err := os.Stat(cleanPath)
    if err != nil {
        return fmt.Errorf("директория не существует: %w", err)
    }
    
    if !info.IsDir() {
        return fmt.Errorf("путь не является директорией")
    }
    
    // Проверяем, что путь в пределах разрешенных директорий
    if !fsm.isPathAllowed(cleanPath) {
        return fmt.Errorf("доступ к директории ограничен")
    }
    
    // Меняем директорию
    if err := os.Chdir(cleanPath); err != nil {
        return fmt.Errorf("не удалось сменить директорию: %w", err)
    }
    
    fsm.currentDir = cleanPath
    return nil
}

// isPathAllowed проверяет, что путь находится в пределах разрешенных директорий
func (fsm *FileSystemManager) isPathAllowed(path string) bool {
    for _, allowed := range fsm.allowedDirs {
        if strings.HasPrefix(path, allowed) {
            return true
        }
    }
    return false
}

// GetCurrentDir возвращает текущую директорию
func (fsm *FileSystemManager) GetCurrentDir() string {
    fsm.mu.RLock()
    defer fsm.mu.RUnlock()
    return fsm.currentDir
}

// ListDir возвращает содержимое директории
func (fsm *FileSystemManager) ListDir(path string) ([]map[string]interface{}, error) {
    fsm.mu.RLock()
    defer fsm.mu.RUnlock()
    
    if path == "" {
        path = fsm.currentDir
    }
    
    if !filepath.IsAbs(path) {
        path = filepath.Join(fsm.currentDir, path)
    }
    
    cleanPath := filepath.Clean(path)
    
    // Проверка безопасности
    if !fsm.isPathAllowed(cleanPath) {
        return nil, fmt.Errorf("доступ к директории ограничен")
    }
    
    entries, err := os.ReadDir(cleanPath)
    if err != nil {
        return nil, err
    }
    
    var result []map[string]interface{}
    for _, entry := range entries {
        info, err := entry.Info()
        if err != nil {
            continue
        }
        
        item := map[string]interface{}{
            "name":    entry.Name(),
            "is_dir":  entry.IsDir(),
            "size":    info.Size(),
            "mode":    info.Mode().String(),
            "mod_time": info.ModTime().Format(time.RFC3339),
        }
        result = append(result, item)
    }
    
    // Сортируем: сначала директории, потом файлы
    sort.Slice(result, func(i, j int) bool {
        if result[i]["is_dir"].(bool) != result[j]["is_dir"].(bool) {
            return result[i]["is_dir"].(bool)
        }
        return strings.ToLower(result[i]["name"].(string)) < 
               strings.ToLower(result[j]["name"].(string))
    })
    
    return result, nil
}

func (ws *WebServer) handleSessionsSave(w http.ResponseWriter, r *http.Request) {
    if r.Method != "POST" {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }
    
    var data struct {
        Name string `json:"name"`
    }
    
    if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
        http.Error(w, "Некорректный JSON", http.StatusBadRequest)
        return
    }
    
    // Используем существующий CommandHandler для сохранения
    ws.assistant.commandHandler.handleSave([]string{data.Name})
    
    response := map[string]interface{}{
        "success": true,
        "message": fmt.Sprintf("Сессия сохранена: %s", data.Name),
        "time":    time.Now().Format(time.RFC3339),
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}

func (ws *WebServer) handleSessionsLoad(w http.ResponseWriter, r *http.Request) {
    if r.Method != "POST" {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }
    
    var data struct {
        Name string `json:"name"`
    }
    
    if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
        http.Error(w, "Некорректный JSON", http.StatusBadRequest)
        return
    }
    
    // Используем существующий CommandHandler для загрузки
    ws.assistant.commandHandler.handleLoad([]string{data.Name})
    
    // Обновляем контекст для всех клиентов после загрузки
    ws.broadcastContext()
    
    response := map[string]interface{}{
        "success": true,
        "message": fmt.Sprintf("Сессия загружена: %s", data.Name),
        "time":    time.Now().Format(time.RFC3339),
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}

func (ws *WebServer) handleSessionsList(w http.ResponseWriter, r *http.Request) {
    if r.Method != "GET" {
        http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
        return
    }
    
    // Используем логику из commands.go для получения списка сессий
    dir := getSessionsDir()
    files, err := os.ReadDir(dir)
    
    var sessions []map[string]interface{}
    
    if err == nil {
        for _, f := range files {
            if !f.IsDir() && strings.HasSuffix(f.Name(), ".json") {
                info, err := f.Info()
                if err != nil {
                    continue
                }
                
                sessions = append(sessions, map[string]interface{}{
                    "name":     strings.TrimSuffix(f.Name(), ".json"),
                    "modified": info.ModTime().Format(time.RFC3339),
                    "size":     info.Size(),
                })
            }
        }
    }
    
    // Сортировка по времени (новые сначала)
    sort.Slice(sessions, func(i, j int) bool {
        t1, _ := time.Parse(time.RFC3339, sessions[i]["modified"].(string))
        t2, _ := time.Parse(time.RFC3339, sessions[j]["modified"].(string))
        return t1.After(t2)
    })
    
    response := map[string]interface{}{
        "success":  true,
        "sessions": sessions,
        "count":    len(sessions),
        "time":     time.Now().Format(time.RFC3339),
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}


// OpenInEditor открывает файл в системном редакторе
func (fsm *FileSystemManager) OpenInEditor(filePath string) error {
    fsm.mu.RLock()
    defer fsm.mu.RUnlock()
    
    if !filepath.IsAbs(filePath) {
        filePath = filepath.Join(fsm.currentDir, filePath)
    }
    
    cleanPath := filepath.Clean(filePath)
    
    // Проверка безопасности
    if !fsm.isPathAllowed(cleanPath) {
        return fmt.Errorf("доступ к файлу ограничен")
    }
    
    // Проверяем существование
    if _, err := os.Stat(cleanPath); os.IsNotExist(err) {
        // Создаем файл, если он не существует
        if err := os.WriteFile(cleanPath, []byte(""), 0644); err != nil {
            return fmt.Errorf("не удалось создать файл: %w", err)
        }
    }
    
    var cmd *exec.Cmd
    switch runtime.GOOS {
    case "darwin":
        cmd = exec.Command("open", cleanPath)
    case "windows":
        cmd = exec.Command("cmd", "/c", "start", "", cleanPath)
    default:
        // Linux/Unix
        editor := os.Getenv("EDITOR")
        if editor == "" {
            editor = "xdg-open"
        }
        cmd = exec.Command(editor, cleanPath)
    }
    
    return cmd.Start()
}



func (ws *WebServer) handleSessions(w http.ResponseWriter, r *http.Request) {
	// TODO: Реализовать управление сессиями через API
	response := map[string]interface{}{
		"sessions": []string{},
		"message":  "Управление сессиями будет реализовано в следующей версии",
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

var startTime = time.Now()
