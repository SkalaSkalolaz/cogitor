// main.go (обновленный)
package main

import (
	"fmt"
	"os"
	"strings"
	"net"
	"strconv"
	"time"
	"path/filepath"

	"github.com/webview/webview_go"
)

const Version = "1.0.1"

func main() {
	// Инициализируем и загружаем конфигурацию
	config := NewConfig()
	config.Load() // Загружаем сохраненные настройки

	// ИНИЦИАЛИЗИРУЕМ ПЕРЕМЕННЫЕ ДО ПАРСИНГА АРГУМЕНТОВ
	provider := "ollama"
	model := "gemma3:4b"
	key := ""
	var inputFile string
	webSearchEnabled := true
	serverMode := false
	serverPort := "8080" // значение по умолчанию
	guiMode := false
	args := os.Args[1:]
	
	// СНАЧАЛА ПАРСИМ ВСЕ АРГУМЕНТЫ
	for i := 0; i < len(args); i++ {
		switch args[i] {
        case "--gui":
            guiMode = true

		case "--server":
			serverMode = true
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				serverPort = args[i+1]
				i++
			}
		case "--provider", "-p":
			if i+1 < len(args) {
				provider = args[i+1]
				i++
			}
		case "--model", "-m":
			if i+1 < len(args) {
				model = args[i+1]
				i++
				if model == "help" {
					switch provider {
					case "openrouter":
						nameModelOpenRouter()
						return
					case "pollinations":
						nameModelPollinations()
						return
					}
				}
			}
		case "--key", "-k":
			if i+1 < len(args) {
				key = args[i+1]
				i++
			}
		case "--no-search", "--disable-search":
			webSearchEnabled = false
		case "--input", "-i":
			if i+1 < len(args) {
				inputFile = args[i+1]
				i++
			}
		case "--help", "-h":
			fmt.Printf("ИИ-ассистент Cogitor v%s\n", Version)
			fmt.Println()
			fmt.Println("Использование:")
			fmt.Println("  cogitor [ПОСТАВЩИК] [МОДЕЛЬ] [API-ключ]")
			fmt.Println("  cogitor [ОПЦИИ]")
			fmt.Println()
			fmt.Println("Режимы работы:")
			fmt.Println("  CLI режим (по умолчанию) - интерактивная командная строка")
			fmt.Println("  Десктопный режим - как GUI приложение")
			fmt.Println("  Серверный режим - веб-интерфейс через браузер")
			fmt.Println()
			fmt.Println("Позиционные аргументы:")
			fmt.Println("  ПОСТАВЩИК          Поставщик LLM (ollama, openrouter, pollinations, phind или URL)")
			fmt.Println("  МОДЕЛЬ             Модель LLM (по умолчанию: gemma3:4b)")
			fmt.Println("  API-ключ           API-ключ (при необходимости)")
			fmt.Println()
			fmt.Println("Опции:")
            fmt.Println("  --gui             Запустить GUI приложение (десктопный режим)")
			fmt.Println("  --server [ПОРТ]   Запустить веб-сервер (порт по умолчанию: 8080)")
			fmt.Println("  -i, --input ФАЙЛ  Файл описания задачи")
			fmt.Println("  -ds, --no-search  Отключить веб-поиск")
			fmt.Println("  -v, --version     Показать версию")
			fmt.Println("  -h, --help        Показать эту справку")
			fmt.Println()
			fmt.Println("Примеры:")
			fmt.Println("  cogitor --server          # Запуск веб-сервера на порту 8080")
			fmt.Println("  cogitor --server 3000     # Запуск веб-сервера на порту 3000")
			fmt.Println("  cogitor ollama qwen2.5-coder:1.5b --gui")
			fmt.Println("  cogitor openrouter mistralai/devstral-2512:free YOUR_KEY --server 9000")
			return
		case "--version", "-v", "version":
			fmt.Printf("AI Cogitor v%s\n", Version)
			return
		default:
			// Позиционные аргументы (старый формат)
			if i == 0 {
				provider = args[i]
			} else if i == 1 {
				model = args[i]
				if model == "help" {
					switch provider {
					case "openrouter":
						nameModelOpenRouter()
						return
					case "pollinations":
						nameModelPollinations()
						return
					}
				}
			} else if i == 2 {
				key = args[i]
			}
		}
	}

	if guiMode {
        startGUI(provider, model, key, webSearchEnabled)
        return
    }
    

	// Если указан режим сервера
	if serverMode {
		// ИСПРАВЛЕНИЕ: передаем полученный порт в startServer
		startServer(provider, model, key, webSearchEnabled, serverPort)
		return
	}

	if inputFile != "" {
		// Автоматический режим
		assistant := NewAssistant(provider, model, key, webSearchEnabled)
		content, err := os.ReadFile(inputFile)
		if err != nil {
			fmt.Printf("❌ Ошибка чтения входного файла: %v\n", err)
			return
		}
		task := strings.TrimSpace(string(content))
		fmt.Printf("🤖 Выполнение задачи из файла: %s\n\n", inputFile)
		autoExecute := config.GetBool("auto_execute")
		assistant.ProcessQuery(task, autoExecute)
		return
	}

	// Интерактивный режим
	assistant := NewAssistant(provider, model, key, webSearchEnabled)
	assistant.RunInteractive()
}

// startServer запускает веб-сервер
func startServer(provider, model, apiKey string, webSearchEnabled bool, port string) {
	fmt.Printf("🤖 Запуск AI Cogitor v%s\n", Version)
	fmt.Printf("🔧 Конфигурация: %s / %s\n", provider, model)

	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	htmlDir := filepath.Join(home, ".cogitor/web")
	path := filepath.Join(home, ".cogitor/web", "index.html")
    // fmt.Println("Home %s  Dir %s  Path %s", home, htmlDir, path)	
	
	// Создаем ассистента
	assistant := NewAssistant(provider, model, apiKey, webSearchEnabled)
	
	// Создаем веб-сервер
	server := NewWebServer(assistant, port)
	
	// Создаем директорию для статических файлов, если ее нет
	if err := os.MkdirAll(htmlDir, 0755); err != nil {
		fmt.Printf("❌ Не удалось создать директорию web: %v\n", err)
		return
	}
	
	// Проверяем наличие index.html
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Println("⚠️  Файл web/index.html не найден. Создайте его для веб-интерфейса.")
		fmt.Println("   Сервер будет работать, но без веб-интерфейса.")
	}
	
	// Запускаем сервер
	fmt.Printf("🚀 Сервер запущен. Откройте http://localhost:%s в браузере\n", port)
	fmt.Println("📡 Для остановки нажмите Ctrl+C")
	
	if err := server.Start(); err != nil {
		fmt.Printf("❌ Ошибка запуска сервера: %v\n", err)
	}
}

func startGUI(provider, model, apiKey string, webSearchEnabled bool) {
    fmt.Printf("🤖 Запуск AI Cogitor GUI v%s\n", Version)
    fmt.Printf("🔧 Конфигурация: %s / %s\n", provider, model)
    
    // 1. Запускаем сервер на случайном порту
    assistant := NewAssistant(provider, model, apiKey, webSearchEnabled)
    server := NewWebServer(assistant, "0") // "0" = случайный свободный порт
    
    // Получаем реальный порт сервера
    listener, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil {
        fmt.Printf("❌ Ошибка запуска сервера: %v\n", err)
        return
    }
    
    port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
    listener.Close()
    
    // Пересоздаем сервер с реальным портом
    server = NewWebServer(assistant, port)
    
    // 2. Запускаем сервер в горутине
    go func() {
        if err := server.StartWithListener("127.0.0.1:" + port); err != nil {
            fmt.Printf("❌ Ошибка сервера: %v\n", err)
        }
    }()
    
    // 3. Даем серверу время запуститься
    time.Sleep(100 * time.Millisecond)
    
    // 4. Запускаем webview
    startWebView("127.0.0.1:" + port)
}

// Вспомогательная функция для запуска webview
func startWebView(url string) {
    debug := true // включить DevTools для отладки
    w := webview.New(debug)
    defer w.Destroy()
    
    w.SetTitle("AI Cogitor - GUI Mode")
    w.SetSize(1200, 880, webview.HintNone)
    w.Navigate("http://" + url)
    
    fmt.Printf("🚀 GUI запущен. URL: http://%s\n", url)
    fmt.Println("📱 Для выхода закройте окно или нажмите Ctrl+C в терминале")
    
    w.Run()
}
    // 
    // w.Run()
// }
