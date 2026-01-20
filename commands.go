// commands.go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"
	"runtime"
	"encoding/json"
	"strconv"
	"sort"
	ctx "context"
	"bytes"
)

// CommandHandler обрабатывает служебные команды
type CommandHandler struct {
	assistant   AssistantAPI
	config      *Config
	stats       *Statistics
	// lastQuery   string
	lastContext string
	terminalReader *TerminalReader
}

// В начале файла после импортов добавить
var commandHelp = map[string]string{
    ":data": `Загрузить файлы данных для RAG-режима
Использование: 
  :data <путь_к_файлу>        — Загрузить один файл
  :data <путь_к_директории>   — Загрузить все текстовые файлы из директории
  :data                       — Выключить RAG-режим и очистить данные
  :data status                — Показать статус загруженных данных

Поддерживаемые форматы: .txt, .json, .csv, .md, .xml, .yaml, .yml
Примеры:
  :data ./data.txt
  :data /path/to/dataset/
  :data ../data/`,
	":clean":     "Очистить всю историю контекста\nИспользование: :clean",
    ":copy": "Включить/выключить автоматическое копирование ответов в буфер обмена\nИспользование: :copy [on|off|status]\nПримеры:\n  :copy on   - включить авто-копирование\n  :copy off  - выключить\n  :copy      - показать статус",
	":pop":       "Удалить последние n обменов из контекста (по умолчанию 1)\nИспользование: :pop [n]",
	":ctx":       "Показать статистику контекста (количество обменов и токенов)\nИспользование: :ctx",
	":limit":     "Установить максимальное количество обменов в контексте\nИспользование: :limit <число>",
	":summarize": "Сжать контекст до 1-2 предложений с помощью LLM\nИспользование: :summarize",
	":save":      "Сохранить текущую сессию в файл\nИспользование: :save [имя]",
	":load":      "Загрузить сессию из файла\nИспользование: :load <имя>",
	":ls":        "Показать список сохраненных сессий\nИспользование: :ls",
	":rm":        "Удалить сохраненную сессию\nИспользование: :rm <имя>",
	":export":    "Экспортировать диалог в файл (форматы: md/txt/json)\nИспользование: :export [fmt]",
	":clip":      "Показать содержимое буфера обмена\nИспользование: :clip",
	":clip+":     "Добавить буфер обмена в следующий запрос\nИспользование: :clip+",
    ":skip":      "Включить/выключить пропуск автоматической установки зависимостей\nИспользование: :skip [on|off]\nКогда включено, программа показывает команды для ручной установки и ожидает нажатия Enter",
	":cd":        "Изменить текущую рабочую директорию\nИспользование: :cd <path>",
	":pwd":       "Показать текущую рабочую директорию\nИспользование: :pwd",
	":dir":       "Показать содержимое директории (аналог ls)\nИспользование: :dir [флаги] [путь]\nФлаги:\n  -a     Показать скрытые файлы\n  -l     Длинный формат (права, размер, дата)\n  -h     Человекочитаемые размеры (с -l)\n  -R     Рекурсивный вывод\nПримеры:\n  :dir              # текущая директория\n  :dir -lh ~/Code   # детальный вывод в '~/Code'\n  :dir -a /tmp      # все файлы в /tmp",
    ":open":      "Открыть файл или директорию в системном редакторе\nИспользование:\n  :open <file>        — Открыть файл\n  :open <dir>         — Открыть директорию как проект\n  :open               — Открыть текущую директорию как проект\nПередает провайдера, модель и API-ключ в редактор для LLM-интеграции",

	":debug":     "Включить/выключить режим отладки\nИспользование: :debug [on|off]",
	":stats":     "Показать статистику использования\nИспользование: :stats",
	":retry":     "Повторить последний запрос\nИспользование: :retry",
	":models":    "Показать доступные модели для текущего провайдера\nИспользование: :models",
	":model": "Изменить модель для текущей сессии\nИспользование: :model <название> (без аргументов показывает текущую)",
    ":providers": "Показать список поддерживаемых LLM провайдеров\nИспользование: :providers",
    ":provider":  "Изменить провайдера для текущей сессии\nИспользование: :provider <название|URL> [модель] [api_key]",
	":set":       "Установить значение настройки\nИспользование: :set <key> <value>",
	":get":       "Показать текущие настройки\nИспользование: :get [key]",
	":reset":     "Сбросить все настройки к значениям по умолчанию\nИспользование: :reset",
	":history":	  "Показать историю последних команд\nИспользование: :history",
	":quit":      "Выйти из программы\nИспользование: :quit",
	":help":      "Показать справку\nИспользование: :help [команда]",
	":sh": `Выполнить shell-команду в терминале\nИспользование: :sh <команда>\nПримеры:\n  :sh ls -lh\n  :sh pwd\n  :sh grep -r "func main" .\n⚠️  ОПАСНО: команды rm, mkfs, dd и др. требуют подтверждения`,
}


func NewCommandHandler(assistant AssistantAPI, config *Config, stats *Statistics, terminalReader *TerminalReader) *CommandHandler {
	return &CommandHandler{
		assistant: assistant,
		config:    config,
		stats:     stats,
		terminalReader: terminalReader,
	}
}

// Handle — главная точка входа для всех команд
func (ch *CommandHandler) Handle(query string) bool {
		if !strings.HasPrefix(query, ":") {
		return false
	}

	parts := strings.Fields(query)
	if len(parts) == 0 || parts[0] == ":" {
		fmt.Println("❌ Не указана команда. Введите :help для списка команд")
		return true // Обработали (ошибочную) команду
	}

	command := parts[0]
	args := parts[1:]

	// Сохраняем для :retry
	// ch.lastQuery = query

	// Добавляем валидацию перед switch
	switch command {
	case ":limit":
		if len(args) < 1 {
			fmt.Println("❌ Использование: :limit <число>")
			return true
		}
		var limit int
		if _, err := fmt.Sscanf(args[0], "%d", &limit); err != nil || limit <= 0 {
			fmt.Println("❌ Недопустимое значение. Укажите положительное число")
			return true
		}
	case ":set":
		if len(args) < 2 {
			fmt.Println("❌ Использование: :set <key> <value>")
			return true
		}
	case ":cd", ":open", ":rm", ":load":
		if len(args) < 1 {
			fmt.Printf("❌ Использование: %s <аргумент>\n", command)
			return true
		}
	}

	switch command {
	case ":clean":
		ch.assistant.GetContext().Clear()
		fmt.Println("✅ Контекст очищен")
	case ":pop":
        n := 1
        if len(args) > 0 {
            if _, err := fmt.Sscanf(args[0], "%d", &n); err != nil {
                fmt.Printf("❌ Недопустимое число: %s\n", args[0])
                return true
            }
            if n <= 0 {
                fmt.Printf("❌ Число должно быть положительным\n")
                return true
            }
        }
        
        if err := ch.assistant.GetContext().Pop(n); err != nil {
            fmt.Printf("❌ Ошибка: %v\n", err)
            return true
        }
        fmt.Printf("✅ Удалено %d последних обменов\n", n)	

	case ":ctx":
        count := ch.assistant.GetContext().GetExchangeCount()
        tokens := ch.assistant.GetContext().GetEstimatedTokens()
        limit := ch.assistant.GetContext().GetMaxLength()
        
        usagePercent := float64(count) / float64(limit) * 100
        
        fmt.Printf("📊 Статистика контекста:\n")
        fmt.Printf("   Обменов: %d / %d (%.0f%%)\n", count, limit, usagePercent)
        fmt.Printf("   Оценка токенов: ~%d\n", tokens)
        
        if count >= limit {
            fmt.Printf("⚠️  ВНИМАНИЕ: Достигнут лимит контекста!\n")
            fmt.Printf("   💡 Используйте :summarize или :clean\n")
        } else if usagePercent >= 80 {
            fmt.Printf("⚠️  Предупреждение: Контекст заполнен на %.0f%%\n", usagePercent)
        }

	case ":limit":
        if len(args) == 0 {
            current := ch.assistant.GetContext().GetMaxLength()
            fmt.Printf("📊 Текущий лимит: %d обменов\n", current)
            return true
        }
        
        var limit int
        if _, err := fmt.Sscanf(args[0], "%d", &limit); err != nil {
            fmt.Printf("❌ Недопустимое значение: %s\n", args[0])
            return true
        }
        
        if err := ch.assistant.GetContext().SetMaxLength(limit); err != nil {
            fmt.Printf("❌ Ошибка: %v\n", err)
            return true
        }
        
        if err := ch.config.Set("context_limit", args[0]); err != nil {
            fmt.Printf("❌ Ошибка сохранения: %v\n", err)
            return true
        }
        
        if err := ch.config.Save(); err != nil {
            fmt.Printf("⚠️  Лимит установлен, но не сохранен: %v\n", err)
        } else {
            fmt.Printf("✅ Лимит установлен и сохранен: %d обменов\n", limit)
        }
    
	case ":summarize":
		autoMode := ch.config.GetBool("auto_execute")
		ch.handleSummarize(autoMode)
    case ":sh":
        ch.handleSh(args)
	case ":dir":
		ch.handleDir(args)
	case ":save":
		ch.handleSave(args)
	case ":load":
		ch.handleLoad(args)
	case ":ls":
		ch.handleListSessions()
	case ":rm":
		ch.handleRemove(args)
	case ":export":
		ch.handleExport(args)
    case ":skip":
    	ch.handleSkipInstall(args)
	case ":clip":
		ch.handleClip()
	case ":clip+":
		ch.handleClipPlus()
	case ":cd":
		ch.handleCD(args)
	case ":pwd":
		ch.handlePWD()
	case ":open":
		ch.handleOpen(args)
	case ":debug":
		ch.handleDebug(args)
	case ":stats":
		ch.stats.Display()
	case ":retry":
		ch.handleRetry()
	case ":models":
		ShowAvailableModels(ch.assistant.GetProvider())
	case ":model":
        ch.handleModel(args)
	case ":providers":
    	ch.handleProviders()
    case ":provider":
    	ch.handleProvider(args)
	case ":history":
    	ch.handleHistory(args)
	case ":set":
		ch.handleSet(args)
	case ":get":
		ch.handleGet(args)
	case ":reset":
		ch.config.Reset()
		fmt.Println("✅ Настройки сброшены")
	case ":quit", ":q":
		fmt.Println("\n🤖 Ассистент: До свидания!")
		os.Exit(0)
	case ":help", ":h":
		ch.showHelp(args)
    case ":copy":
        ch.handleCopyCommand(args)
    case ":data":
        ch.handleData(args)
	default:
		fmt.Printf("❌ Неизвестная команда: %s\nНаберите :help для списка команд\n", command)
	}
	return true
}

// Добавим обработчик команды :copy
func (ch *CommandHandler) handleCopyCommand(args []string) {
    if len(args) == 0 {
        // Показать статус
        enabled := ch.assistant.(*Assistant).GetAutoCopyEnabled()
        status := "выключено"
        if enabled {
            status = "включено"
        }
        fmt.Printf("📋 Авто-копирование ответов: %s\n", status)
        return
    }
    
    switch strings.ToLower(args[0]) {
    case "on", "true", "enable", "вкл", "да":
        ch.assistant.(*Assistant).SetAutoCopyEnabled(true)
        fmt.Println("✅ Авто-копирование ответов включено")
        // Сохраняем в конфиг для будущих сессий
        ch.config.Set("auto_copy_responses", "true")
        ch.config.Save()
        
    case "off", "false", "disable", "выкл", "нет":
        ch.assistant.(*Assistant).SetAutoCopyEnabled(false)
        fmt.Println("✅ Авто-копирование ответов выключено")
        // Сохраняем в конфиг для будущих сессий
        ch.config.Set("auto_copy_responses", "false")
        ch.config.Save()
        
    case "status", "stat", "статус":
        enabled := ch.assistant.(*Assistant).GetAutoCopyEnabled()
        status := "выключено"
        if enabled {
            status = "включено"
        }
        fmt.Printf("📋 Авто-копирование ответов: %s\n", status)
        
    default:
        fmt.Printf("❌ Недопустимый параметр: %s\n", args[0])
        fmt.Println("Используйте: on, off или status")
    }
}

// Добавляем новую функцию для обработки команды :data:
func (ch *CommandHandler) handleData(args []string) {
    // Если мы в веб-режиме, используем API
    // if ch.isWebMode() {
        // ch.handleDataWeb(args)
        // return
    // }
// 
    if len(args) == 0 {
        // Выключить RAG-режим
        ch.assistant.(*Assistant).ClearRAGData()
        fmt.Println("✅ RAG-режим выключен, данные очищены")
        return
    }
    
    // Проверка статуса
    if args[0] == "status" || args[0] == "stat" {
        ch.showRAGStatus()
        return
    }
    
    path := args[0]
    resolvedPath := ch.resolveDirectoryPath(path)
    
    // Проверяем существование
    info, err := os.Stat(resolvedPath)
    if err != nil {
        fmt.Printf("❌ Ошибка доступа к пути '%s': %v\n", path, err)
        return
    }
    
    var docs []RAGDocument
    if info.IsDir() {
        // Загружаем все файлы из директории
        docs = ch.loadFilesFromDirectory(resolvedPath)
    } else {
        // Загружаем один файл
        doc, err := ch.loadSingleFile(resolvedPath)
        if err != nil {
            fmt.Printf("❌ Ошибка загрузки файла: %v\n", err)
            return
        }
        docs = []RAGDocument{doc}
    }
    
    if len(docs) == 0 {
        fmt.Println("⚠️  Не найдено подходящих файлов для загрузки")
        return
    }
    
    // Устанавливаем данные в Assistant
    ch.assistant.(*Assistant).SetRAGData(docs)
    
    // Показываем статистику
    totalSize := 0
    for _, doc := range docs {
        totalSize += doc.Size
    }
    
    fmt.Printf("✅ RAG-режим активирован\n")
    fmt.Printf("📊 Загружено документов: %d\n", len(docs))
    fmt.Printf("📊 Общий размер: %d символов\n", totalSize)
    fmt.Printf("📊 Использование: данные будут использоваться для всех последующих запросов\n")
    fmt.Printf("💡 Для отключения введите: :data\n")
}

// handleDataWeb обрабатывает команду :data в веб-режиме
func (ch *CommandHandler) handleDataWeb(args []string) {
    // Перенаправляем на JavaScript функцию
    fmt.Println("ℹ️  Используйте кнопки в веб-интерфейсе для управления RAG режимом")
    fmt.Println("   или откройте веб-интерфейс по адресу: http://localhost:8080")
}

// Вспомогательные функции для загрузки файлов:
func (ch *CommandHandler) loadSingleFile(filePath string) (RAGDocument, error) {
    content, err := os.ReadFile(filePath)
    if err != nil {
        return RAGDocument{}, err
    }
    
    // Проверяем поддерживаемый формат
    if !isSupportedRAGFile(filePath) {
        return RAGDocument{}, fmt.Errorf("неподдерживаемый формат файла")
    }
    
    return RAGDocument{
        FilePath: filePath,
        Content:  string(content),
        Size:     len(content),
        LoadedAt: time.Now(),
    }, nil
}

func (ch *CommandHandler) loadFilesFromDirectory(dirPath string) []RAGDocument {
    var docs []RAGDocument
    
    files, err := os.ReadDir(dirPath)
    if err != nil {
        fmt.Printf("❌ Ошибка чтения директории: %v\n", err)
        return docs
    }
    
    supportedExtensions := []string{".txt", ".json", ".csv", ".md", ".xml", ".yaml", ".yml"}
    
    for _, file := range files {
        if file.IsDir() {
            continue
        }
        
        filePath := filepath.Join(dirPath, file.Name())
        
        // Проверяем расширение
        ext := strings.ToLower(filepath.Ext(file.Name()))
        supported := false
        for _, supportedExt := range supportedExtensions {
            if ext == supportedExt {
                supported = true
                break
            }
        }
        
        if !supported {
            continue
        }
        
        // Проверяем размер файла (ограничение 1MB)
        info, err := file.Info()
        if err != nil || info.Size() > 1024*1024 {
            fmt.Printf("⚠️  Файл %s слишком большой или недоступен, пропускаем\n", file.Name())
            continue
        }
        
        doc, err := ch.loadSingleFile(filePath)
        if err != nil {
            fmt.Printf("⚠️  Ошибка загрузки файла %s: %v\n", file.Name(), err)
            continue
        }
        
        docs = append(docs, doc)
    }
    
    return docs
}

func (ch *CommandHandler) showRAGStatus() {
    assistant, ok := ch.assistant.(*Assistant)
    if !ok {
        fmt.Println("❌ Ошибка получения состояния RAG")
        return
    }
    
    docs := assistant.GetRAGData()
    enabled := assistant.IsRAGEnabled()
    
    fmt.Printf("📊 Состояние RAG-режима:\n")
    fmt.Printf("   Статус: ")
    if enabled {
        fmt.Printf("✅ ВКЛЮЧЕН\n")
    } else {
        fmt.Printf("❌ ВЫКЛЮЧЕН\n")
    }
    
    if enabled && len(docs) > 0 {
        totalSize := 0
        for _, doc := range docs {
            totalSize += doc.Size
        }
        
        fmt.Printf("📊 Загружено документов: %d\n", len(docs))
        fmt.Printf("📊 Общий размер данных: %d символов\n", totalSize)
        fmt.Printf("📊 Список файлов:\n")
        for i, doc := range docs {
            fmt.Printf("   %d. %s (%d символов, загружен %s)\n", 
                i+1, doc.FilePath, doc.Size, 
                doc.LoadedAt.Format("02.01.2006 15:04"))
        }
    } else {
        fmt.Printf("📊 Данные не загружены\n")
    }
}

func isSupportedRAGFile(filePath string) bool {
    ext := strings.ToLower(filepath.Ext(filePath))
    supported := []string{".txt", ".json", ".csv", ".md", ".xml", ".yaml", ".yml"}
    
    for _, supportedExt := range supported {
        if ext == supportedExt {
            return true
        }
    }
    return false
}

func (ch *CommandHandler) handleSkipInstall(args []string) {
	if len(args) == 0 {
		skipMode, ok := ch.config.Get("skip_install")
		if !ok {
			fmt.Printf("📊 Режим пропуска установки: не установлен\n")
			return
		}
		if mode, ok := skipMode.(bool); ok {
			status := "выключен"
			if mode {
				status = "включен"
			}
			fmt.Printf("📊 Режим пропуска установки: %s\n", status)
		} else {
			fmt.Printf("📊 Режим пропуска установки: %v (тип: %T)\n", skipMode, skipMode)
		}
		return
	}

	switch args[0] {
	case "on", "true":
		ch.config.Set("skip_install", "true")
		ch.config.Save()
		fmt.Println("✅ Режим пропуска установки включен")
	case "off", "false":
		ch.config.Set("skip_install", "false")
		ch.config.Save()
		fmt.Println("✅ Режим пропуска установки выключен")
	default:
		fmt.Printf("❌ Недопустимое значение: %s\n", args[0])
		fmt.Println("Используйте: on/true или off/false")
	}
}

// handleDir обрабатывает команду отображения содержимого директории
func (ch *CommandHandler) handleDir(args []string) {
	// Парсим аргументы: отделяем флаги от пути
	flags := []string{}
	dirPath := ""

	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
		} else if dirPath == "" {
			dirPath = arg
		}
	}

	// Если путь не указан, используем текущую директорию
	if dirPath == "" {
		dirPath = "."
	}

	// Разрешаем путь (поддержка ~/, абсолютных и относительных путей)
	resolvedPath := ch.resolveDirectoryPath(dirPath)

	// Проверяем существование директории
	info, err := os.Stat(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("❌ Директория не найдена: %s\n", dirPath)
			return
		}
		fmt.Printf("❌ Ошибка доступа к директории: %v\n", err)
		return
	}

	if !info.IsDir() {
		fmt.Printf("❌ Указанный путь не является директорией: %s\n", dirPath)
		return
	}

	// Обрабатываем флаги
	showHidden := false
	longFormat := false
	humanReadable := false
	recursive := false

	for _, flag := range flags {
		if strings.Contains(flag, "a") {
			showHidden = true
		}
		if strings.Contains(flag, "l") {
			longFormat = true
		}
		if strings.Contains(flag, "h") {
			humanReadable = true
		}
		if strings.Contains(flag, "R") {
			recursive = true
		}
	}

	// Выводим содержимое директории
	fmt.Printf("📁 Содержимое директории: %s\n", resolvedPath)
	if err := ch.printDirectory(resolvedPath, showHidden, longFormat, humanReadable, recursive, 0); err != nil {
		fmt.Printf("❌ Ошибка чтения директории: %v\n", err)
	}
}

// resolveDirectoryPath разрешает путь директории (поддержка ~/, абсолютных и относительных путей)
func (ch *CommandHandler) resolveDirectoryPath(path string) string {
	// Обработка домашней директории
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}

	// Абсолютный путь
	if filepath.IsAbs(path) {
		return path
	}

	// Относительный путь
	absPath, err := filepath.Abs(path)
	if err != nil {
		return path // Возвращаем исходный в случае ошибки
	}

	return absPath
}

// printDirectory выводит содержимое директории с учетом флагов
func (ch *CommandHandler) printDirectory(dirPath string, showHidden, longFormat, humanReadable, recursive bool, depth int) error {
	// Читаем содержимое директории
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return err
	}

	// Фильтруем скрытые файлы
	var filtered []os.DirEntry
	for _, entry := range entries {
		if !showHidden && strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		filtered = append(filtered, entry)
	}

	// Сортируем: сначала директории, затем файлы
	sort.Slice(filtered, func(i, j int) bool {
		iIsDir, jIsDir := filtered[i].IsDir(), filtered[j].IsDir()
		if iIsDir != jIsDir {
			return iIsDir // Директории идут первыми
		}
		return filtered[i].Name() < filtered[j].Name()
	})

	// Выводим содержимое
	if depth > 0 {
		fmt.Printf("\n%s:\n", dirPath)
	}

	for _, entry := range filtered {
		fullPath := filepath.Join(dirPath, entry.Name())

		if longFormat {
			// Длинный формат: [права] [размер] [дата] [имя]
			info, err := os.Stat(fullPath)
			if err != nil {
				continue // Пропускаем при ошибке
			}

			mode := info.Mode()
			size := info.Size()
			modTime := info.ModTime().Format("2006-01-02 15:04")

			// Форматируем размер
			sizeStr := fmt.Sprintf("%d", size)
			if humanReadable {
				sizeStr = formatSize(size)
			}

			// Форматируем вывод
			if entry.IsDir() {
				fmt.Printf("  %s  %8s  %s  📁 %s/\n", 
					mode, sizeStr, modTime, entry.Name())
			} else {
				fmt.Printf("  %s  %8s  %s  📄 %s\n", 
					mode, sizeStr, modTime, entry.Name())
			}
		} else {
			// Короткий формат: просто имена
			if entry.IsDir() {
				fmt.Printf("  📁 %s/\n", entry.Name())
			} else {
				fmt.Printf("  📄 %s\n", entry.Name())
			}
		}

		// Рекурсивный вывод для поддиректорий
		if recursive && entry.IsDir() {
			ch.printDirectory(fullPath, showHidden, longFormat, humanReadable, recursive, depth+1)
		}
	}

	return nil
}

// formatSize конвертирует размер в человекочитаемый формат
func formatSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%dB", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(size)/float64(div), "KMGTPE"[exp])
}

func (ch *CommandHandler) handleHistory(args []string) {
    if len(args) == 0 {
        // Стандартный вывод истории
        history := ch.terminalReader.GetHistory()
        if len(history) == 0 {
            fmt.Println("📋 История пуста")
            return
        }
        fmt.Printf("📋 История (последние %d команд):\n", len(history))
        for i, cmd := range history {
            fmt.Printf("  %d: %s\n", i+1, cmd)
        }
        return
    }

    // Подкоманды
    switch args[0] {
    case "save":
        if len(args) < 2 {
            fmt.Println("❌ Использование: :history save <имя>")
            return
        }
        ch.saveHistoryToFile(args[1])
    case "clear":
        ch.clearHistory()
    default:
        fmt.Printf("❌ Неизвестная подкоманда '%s'\n", args[0])
        fmt.Println("Использование: :history [save <имя>|clear]")
    }
}

func (ch *CommandHandler) handleModel(args []string) {
    if len(args) == 0 {
        fmt.Printf("📊 Текущая модель: %s\n", ch.assistant.GetModel())
        return
    }
    
    newModel := args[0]
    oldModel := ch.assistant.GetModel()
    
    ch.assistant.SetModel(newModel)
    fmt.Printf("✅ Модель изменена: %s → %s (только для текущей сессии)\n", oldModel, newModel)
}
// В commands.go добавить вспомогательные методы:
func (ch *CommandHandler) saveHistoryToFile(name string) {
    history := ch.terminalReader.GetHistory()
    if len(history) == 0 {
        fmt.Println("⚠️  История пуста")
        return
    }
    filename := fmt.Sprintf("history_%s.txt", name)
    file, err := os.Create(filename)
    if err != nil {
        fmt.Printf("❌ Ошибка: %v\n", err)
        return
    }
    defer file.Close()
    
    for i, cmd := range history {
        fmt.Fprintf(file, "%d: %s\n", i+1, cmd)
    }
    fmt.Printf("✅ История сохранена: %s\n", filename)
}

func (ch *CommandHandler) clearHistory() {
    response, err := ch.terminalReader.ReadLineWithPrompt("Очистить историю? (y/n): ")
    if err != nil || strings.ToLower(strings.TrimSpace(response)) != "y" {
        return
    }
    // Добавить метод ClearHistory в TerminalReader
    ch.terminalReader.ClearHistory()
    fmt.Println("✅ История очищена")
}

// ========== Методы контекста ==========

func (ch *CommandHandler) handleSummarize(autoMode bool) {
    if ch.assistant.GetContext().GetExchangeCount() == 0 {
        fmt.Println("⚠️ Контекст пуст, нечего суммаризировать")
        return
    }

    if !autoMode {
        fmt.Sprintf("Сжаты обмены с LLM. Оригинал будет потерян.")
        // count := ch.assistant.GetContext().GetExchangeCount()
        // response, err := ch.terminalReader.ReadLineWithPrompt(
            // fmt.Sprintf("Сжать %d обменов? Оригинал будет потерян. (y/n): ", count))
        // if err != nil || strings.ToLower(strings.TrimSpace(response)) != "y" {
            // fmt.Println("❌ Суммаризация отменена")
            // return
        // }
    }

    // Сохраняем оригинал для возможной отмены
    original := ch.assistant.GetContext().GetAllExchanges()
    backup := strings.Join(original, "|EXCHANGE_BREAK|")
    ch.config.Set("summarize_backup", backup)
    ch.config.Save()

    // Выполняем суммаризацию
    context := ch.assistant.GetContext().GetContext()
    prompt := fmt.Sprintf(`Сожми диалог до 2-4 предложений:
    
    %s
    
    ВЕРНИ ТОЛЬКО сводку.`, context)
	response, err := SendMessageToLLM(ctx.Background(), prompt, ch.assistant.GetProvider(), ch.assistant.GetModel(), ch.assistant.GetAPIKey())
    if err != nil {
        fmt.Printf("❌ Ошибка: %v\n", err)
        return
    }

    // Проверяем длину ответа
    if len(response) > 2000 {
        response = response[:2000] + "...(обрезано)"
    }

    ch.assistant.GetContext().Clear()
    ch.assistant.GetContext().AddExchange("Сводка диалога", response)
    fmt.Printf("📋 Сводка: %s\n", response)
    // fmt.Printf("💡 Для отмены используйте :pop\n")
}


// ========== Методы сессий ==========

func getSessionsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cogitor", "sessions")
}

func (ch *CommandHandler) handleSave(args []string) {
	name := "session"
    if len(args) > 0 {                       // ← защита от пустого слайса
        name = args[0]
    }

    dir := getSessionsDir()                  // ← теперь dir виден везде
    if err := os.MkdirAll(dir, 0755); err != nil {
        fmt.Printf("❌ Ошибка создания директории: %v\n", err)
        return
    }

	if len(args) > 0 && args[0] != "" {
        // Проверка на недопустимые символы
        if strings.ContainsAny(args[0], "/\\:*?\"<>|") {
            fmt.Printf("❌ Недопустимые символы в имени сессии (используйте буквы, цифры, -, _)\n")
            return
        }
        // Ограничение длины
        if len(args[0]) > 50 {
            fmt.Printf("❌ Имя сессии слишком длинное (макс. 50 символов)\n")
            return
        }
    }
    
    // Проверка существования файла
    path := filepath.Join(dir, name+".json")
    if _, err := os.Stat(path); err == nil {
        response, err := ch.terminalReader.ReadLineWithPrompt(
            fmt.Sprintf("Сессия '%s' уже существует. Перезаписать? (y/n): ", name))
        if err != nil || strings.ToLower(strings.TrimSpace(response)) != "y" {
            fmt.Println("❌ Сохранение отменено")
            return
        }
    }
    data := SessionData{
        Version:   SessionFormatVersion,
        Timestamp: time.Now().Format(time.RFC3339),
        Provider:  ch.assistant.GetProvider(),
        Model:     ch.assistant.GetModel(),
        Exchanges: ch.assistant.GetContext().GetAllExchanges(),
    }
	jsonData, err := json.MarshalIndent(data, "", "  ")
    if err != nil {
        fmt.Printf("❌ Ошибка сериализации: %v\n", err)
        return
    }
    // Атомарная запись через временный файл (заменить os.WriteFile)
    tempPath := path + ".tmp"
    if err = os.WriteFile(tempPath, jsonData, 0644); err != nil {
        fmt.Printf("❌ Ошибка сохранения: %v\n", err)
        os.Remove(tempPath) // cleanup
        return
    }
    // Атомарное перемещение
    if err := os.Rename(tempPath, path); err != nil {
        fmt.Printf("❌ Ошибка завершения сохранения: %v\n", err)
        return
    }

	fmt.Printf("✅ Сессия сохранена: %s\n", path)
}

func (ch *CommandHandler) handleLoad(args []string) {
	if len(args) == 0 {
		fmt.Println("❌ Укажите имя сессии: :load <имя>")
		return
	}

	// Проверка существования файла
	path := filepath.Join(getSessionsDir(), args[0]+".json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Printf("❌ Сессия '%s' не найдена\n", args[0])
		return
	}

	// Подтверждение перед потерей текущего контекста
	if ch.assistant.GetContext().GetExchangeCount() > 0 {
		response, err := ch.terminalReader.ReadLineWithPrompt(
			"Текущий контекст будет потерян. Продолжить загрузку? (y/n): ")
		if err != nil || strings.ToLower(strings.TrimSpace(response)) != "y" {
			fmt.Println("❌ Загрузка отменена")
			return
		}
	}

	fileData, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("❌ Ошибка загрузки: %v\n", err)
		return
	}

	var data struct {
		Version   string   `json:"version"`   // добавлено поле версии
		Timestamp string   `json:"timestamp"`
		Provider  string   `json:"provider"`
		Model     string   `json:"model"`
		Exchanges []string `json:"exchanges"`
	}

	if err := json.Unmarshal(fileData, &data); err != nil {
		fmt.Printf("❌ Ошибка парсинга JSON: %v\n", err)
		return
	}

	// Проверка версии формата сессии
	if data.Version != "" && data.Version != SessionFormatVersion {
		fmt.Printf("⚠️  Предупреждение: Сессия в формате v%s, текущая v%s. Могут быть проблемы совместимости.\n",
			data.Version, SessionFormatVersion)
	}

	// Валидация структуры после Unmarshal
	if data.Provider == "" || data.Model == "" {
		fmt.Printf("⚠️  Предупреждение: Сессия '%s' имеет неполные метаданные\n", args[0])
	}
	if len(data.Exchanges) == 0 {
		fmt.Printf("⚠️  Предупреждение: Сессия '%s' пуста\n", args[0])
	}

	// Восстанавливаем контекст
	ch.assistant.GetContext().LoadFromHistory(data.Exchanges)

	// Информируем о возможных различиях в провайдере/модели
	if data.Provider != ch.assistant.GetProvider() || data.Model != ch.assistant.GetModel() {
		fmt.Printf("⚠️  Внимание: Сессия сохранена с %s/%s\n", data.Provider, data.Model)
		fmt.Printf("   Текущая конфигурация: %s/%s\n", ch.assistant.GetProvider(), ch.assistant.GetModel())
	}

	fmt.Printf("✅ Сессия загружена: %s (обменов: %d)\n", path, len(data.Exchanges))
}

func (ch *CommandHandler) handleListSessions() {
	dir := getSessionsDir()
	files, err := os.ReadDir(dir)

	if err != nil {
        if os.IsNotExist(err) {
            fmt.Println("📁 Сохраненные сессии отсутствуют")
            return
        }
        fmt.Printf("❌ Ошибка чтения директории: %v\n", err)
        return
    }
    
    // Сбор и сортировка сессий
    type sessionInfo struct {
        name string
        modTime time.Time
    }
    var sessions []sessionInfo
    
    for _, f := range files {
        if !f.IsDir() && strings.HasSuffix(f.Name(), ".json") {
            info, err := f.Info()
            if err != nil {
                continue // Пропускаем файлы с ошибками
            }
            sessions = append(sessions, sessionInfo{
                name: strings.TrimSuffix(f.Name(), ".json"),
                modTime: info.ModTime(),
            })
        }
    }
    
    if len(sessions) == 0 {
        fmt.Println("📁 Сохраненные сессии отсутствуют")
        return
    }
    
    // Сортировка по времени (сначала новые)
    sort.Slice(sessions, func(i, j int) bool {
        return sessions[i].modTime.After(sessions[j].modTime)
    })
    
    // Красивый вывод
    fmt.Printf("📁 Сохраненные сессии (%d):\n", len(sessions))
    for _, s := range sessions {
        fmt.Printf("  %-20s %s\n", s.name, s.modTime.Format("2006-01-02 15:04"))
    }

	if err != nil {
		fmt.Printf("❌ Ошибка чтения директории: %v\n", err)
		return
	}
}

// Альтернативный вариант - полностью убрать подтверждение:
func (ch *CommandHandler) handleRemove(args []string) {
    if len(args) == 0 {
        fmt.Println("❌ Укажите имя сессии: :rm <имя>")
        return
    }

    sessionName := args[0]
    
    // Валидация имени (аналогично save)
    if strings.ContainsAny(sessionName, "/\\:*?\"<>|") {
        fmt.Printf("❌ Недопустимое имя сессии\n")
        return
    }
    
    path := filepath.Join(getSessionsDir(), sessionName+".json")
    if _, err := os.Stat(path); os.IsNotExist(err) {
        fmt.Printf("❌ Сессия '%s' не найдена\n", sessionName)
        return
    }
    
    // Читаем содержимое файла для возможного восстановления
    fileContent, _ := os.ReadFile(path)
    
    // Перемещение в "корзину" вместо немедленного удаления
    trashDir := filepath.Join(getSessionsDir(), ".trash")
    os.MkdirAll(trashDir, 0755)
    
    // Создаем уникальное имя для файла в корзине
    timestamp := time.Now().Format("20060102_150405")
    trashPath := filepath.Join(trashDir, fmt.Sprintf("%s_%s.json", sessionName, timestamp))
    
    // Сохраняем метаданные для возможного восстановления
    metadata := map[string]string{
        "original_name": sessionName,
        "deleted_at":    time.Now().Format(time.RFC3339),
        "original_path": path,
        "content":       string(fileContent),
    }
    
    metadataJSON, _ := json.MarshalIndent(metadata, "", "  ")
    metadataPath := trashPath + ".meta"
    os.WriteFile(metadataPath, metadataJSON, 0644)
    
    if err := os.Rename(path, trashPath); err != nil {
        fmt.Printf("❌ Ошибка удаления: %v\n", err)
        return
    }
    
    fmt.Printf("✅ Сессия '%s' удалена\n", sessionName)
}

func (ch *CommandHandler) handleExport(args []string) {
    format := "md"
    if len(args) > 0 {
        format = args[0]
    }

    filename := fmt.Sprintf("export_%s.%s", time.Now().Format("20060102_150405"), format)
    file, err := os.Create(filename)
    if err != nil {
        fmt.Printf("❌ Ошибка создания файла: %v\n", err)
        return
    }
    defer file.Close()

    content := ch.assistant.GetContext().GetAllExchanges()
    var writeErr error

    switch format {
    case "md":
        _, writeErr = file.WriteString("# Экспорт сессии\n\n")
        for _, ex := range content {
            _, _ = file.WriteString("---\n")
            _, _ = file.WriteString(ex + "\n\n")
        }
    case "txt":
        for _, ex := range content {
            _, _ = file.WriteString(ex + "\n\n")
        }
    case "json":
        _, _ = file.WriteString(`{ "exchanges": [`)
        for i, ex := range content {
            if i > 0 {
                _, _ = file.WriteString(",")
            }
            _, _ = file.WriteString(fmt.Sprintf("%q", ex))
        }
        _, _ = file.WriteString("]}")
    }

    if writeErr != nil {
        fmt.Printf("❌ Ошибка записи файла: %v\n", writeErr)
        _ = os.Remove(filename) // удаляем повреждённый файл
        return
    }

    fmt.Printf("✅ Диалог экспортирован: %s\n", filename)
}

// ========== Методы I/O ==========

func (ch *CommandHandler) handleClip() {
	content, err := ReadClipboard()
	if err != nil {
		fmt.Printf("❌ Ошибка: %v\n", err)
		return
	}
	fmt.Printf("📋 Буфер обмена (%d символов):\n%s\n", len(content), content)
}

func (ch *CommandHandler) handleClipPlus() {
	content, err := ReadClipboard()
	if err != nil {
		fmt.Printf("❌ Ошибка: %v\n", err)
		return
	}
	ch.assistant.GetContext().AddExchange("Буфер обмена", content)
	fmt.Println("✅ Буфер добавлен в следующий запрос")
}

func (ch *CommandHandler) handleCD(args []string) {
	if len(args) == 0 {
		fmt.Println("❌ Укажите директорию: :cd <path>")
		return
	}

	if err := os.Chdir(args[0]); err != nil {
		fmt.Printf("❌ Ошибка смены директории: %v\n", err)
		return
	}

	pwd, _ := os.Getwd()
	fmt.Printf("✅ Текущая директория: %s\n", pwd)
}

func (ch *CommandHandler) handlePWD() {
	pwd, _ := os.Getwd()
	fmt.Println(pwd)
}

// handleOpen открывает файл/директорию в редакторе с передачей конфигурации LLM
// handleOpen открывает файл/директорию в редакторе с передачей конфигурации LLM
func (ch *CommandHandler) handleOpen(args []string) {
	var targetPath string
	
	if len(args) == 0 {
		// Открываем текущую директорию как проект
		pwd, err := os.Getwd()
		if err != nil {
			fmt.Printf("❌ Ошибка получения текущей директории: %v\n", err)
			return
		}
		targetPath = pwd
		fmt.Printf("📂 Открытие текущей директории как проекта: %s\n", targetPath)
	} else {
		targetPath = args[0]
		
		// Проверяем, существует ли путь
		info, err := os.Stat(targetPath)
		if err != nil {
			if os.IsNotExist(err) {
				// Если файл не существует, создаем новый файл
				fmt.Printf("⚠️  Файл не найден: %s. Будет создан новый файл.\n", targetPath)
			} else {
				fmt.Printf("❌ Ошибка доступа к пути: %v\n", err)
				return
			}
		} else {
			// Путь существует
			if info.IsDir() {
				fmt.Printf("📂 Открытие директории как проекта: %s\n", targetPath)
			} else {
				fmt.Printf("📄 Открытие файла: %s\n", targetPath)
			}
		}
	}

	// Получаем конфигурацию для передачи в редактор
	provider := ch.assistant.GetProvider()
	model := ch.assistant.GetModel()
	apiKey := ch.assistant.GetAPIKey()

	// Определяем редактор
	editor := os.Getenv("EDITOR")
	if editor == "" {
		switch runtime.GOOS {
		case "darwin", "linux":
			editor = "editor"
		case "windows":
			editor = "notepad.exe"
		}
	}

	// Формируем аргументы для редактора
	// Формат: editor [provider]/[URL provider] [model] [path] [sn-...]
	editorArgs := []string{}
	editorArgs = append(editorArgs, provider)
	editorArgs = append(editorArgs, model)
	editorArgs = append(editorArgs, targetPath)
	if apiKey != "" {
		editorArgs = append(editorArgs, apiKey)
	}

	// Сохраняем историю и закрываем терминал перед запуском редактора
	history := ch.terminalReader.GetHistory()
	ch.terminalReader.Close()
	
	// Запускаем редактор с аргументами
	cmd := exec.Command(editor, editorArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Printf("❌ Ошибка запуска редактора: %v\n", err)
		fmt.Printf("   Команда: %s %s\n", editor, strings.Join(editorArgs, " "))
	}
	
	// Восстанавливаем терминал после выхода из редактора
	ch.terminalReader = NewTerminalReader("👤 Вы: ", 20)
	// Восстанавливаем историю и автодополнение
	for _, h := range history {
		ch.terminalReader.line.AppendHistory(h)
	}
	commands := []string{
		":clean", ":pop", ":ctx", ":limit", ":summarize", //":undo",
		":save", ":load", ":ls", ":rm", ":export", ":sh",
		":clip", ":clip+", ":cd", ":pwd", ":open", ":dir",
		":debug", ":stats", ":retry", ":models", ":model", ":providers", ":provider",
		":set", ":get", ":reset", ":quit", ":help", ":history", ":skip",
	}
	ch.terminalReader.SetCompleter(commands)
	
	fmt.Printf("✅ Редактор закрыт\n")
}

// ========== Методы отладки ==========

func (ch *CommandHandler) handleDebug(args []string) {
	if len(args) == 0 {
	debugMode, ok := ch.config.Get("debug_mode")
	if !ok {
		fmt.Printf("📊 Debug mode: не установлен\n")
		return
	}
	// Приведение типа с проверкой
	if mode, ok := debugMode.(bool); ok {
		fmt.Printf("📊 Debug mode: %v\n", mode)
	} else {
		fmt.Printf("📊 Debug mode: %v (тип: %T)\n", debugMode, debugMode)
	}
	return
}
	
	switch args[0] {
	case "on", "true":
		ch.config.Set("debug_mode", "true")
		fmt.Println("✅ Debug mode включен")
	case "off", "false":
		ch.config.Set("debug_mode", "false")
		fmt.Println("✅ Debug mode выключен")
	default:
		fmt.Printf("❌ Недопустимое значение: %s\n", args[0])
	}
}

func (ch *CommandHandler) handleRetry() {
	// Используем lastUserQuery из Assistant, а не из CommandHandler
	if ch.assistant.GetLastUserQuery() == "" {
		fmt.Println("⚠️  Нет запроса для повтора")
		return
	}
	fmt.Printf("🔄 Повтор: %s\n", ch.assistant.GetLastUserQuery())
	ch.assistant.ProcessQuery(ch.assistant.GetLastUserQuery(), false)
}

// shortHost возвращает первую часть имени хоста (до первой точки)
func shortHost() string {
	h, _ := os.Hostname()
	if i := strings.IndexByte(h, '.'); i > 0 {
		h = h[:i]
	}
	return h
}

// promptString собирает строку вида user@host:dir$
func promptString() string {
	u, _ := user.Current()
	host := shortHost()
	dir, _ := os.Getwd()

	// для Windows используем обратный слэш и «>»
	if runtime.GOOS == "windows" {
		dir = strings.ReplaceAll(dir, "/", "\\")
		return fmt.Sprintf("%s@%s:%s>", u.Username, host, dir)
	}
	return fmt.Sprintf("%s@%s:%s$", u.Username, host, dir)
}

func (ch *CommandHandler) handleSh(args []string) {
    if len(args) == 0 {
        fmt.Println("❌ Использование: :sh <команда>")
        return
    }
    
    // Собираем полную команду из аргументов
    command := strings.Join(args, " ")
    
    // Базовая валидация на опасные команды
    dangerousKeywords := []string{
        "rm ", "dd ", "mkfs", "fdisk", "shred", "chmod -R 777",
        "curl", "wget", ">", ">>", "&&", ";",
    }
    
    needsConfirmation := false
    for _, dangerous := range dangerousKeywords {
        if strings.Contains(command, dangerous) {
            needsConfirmation = true
            break
        }
    }
    
    // Запрос подтверждения для опасных команд
    if needsConfirmation {
        response, err := ch.terminalReader.ReadLineWithPrompt(
            fmt.Sprintf("⚠️  Опасная команда '%s'. Выполнить? (y/n): ", command))
        if err != nil || strings.ToLower(strings.TrimSpace(response)) != "y" {
            fmt.Println("❌ Команда отменена пользователем")
            return
        }
    }
    
    // fmt.Printf("🚀 Выполнение: %s\n", command)
    fmt.Printf("%s %s\n", promptString(), command)
    
	// --- перехват вывода ---
	var out, errOut bytes.Buffer
	cmd := exec.Command("sh", "-c", command)
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	cmd.Stdin = nil // в канвасе интерактив не нужен

	if err := cmd.Run(); err != nil {
		fmt.Printf("❌ Ошибка выполнения: %v\n", err)
	}
	if errOut.Len() > 0 {
		fmt.Printf("stderr:\n%s\n", errOut.String())
	}
	if out.Len() > 0 {
		fmt.Printf("stdout:\n%s\n", out.String())
	}
}

// ========== Методы конфигурации ==========

func (ch *CommandHandler) handleSet(args []string) {
	if len(args) < 2 {
		fmt.Println("❌ Использование: :set <key> <value>")
		return
	}

	key := args[0]
	value := args[1]

	if err := ch.config.Set(key, value); err != nil {
		fmt.Printf("❌ Ошибка: %v\n", err)
		return
	}

	// 🔄 НЕМЕДЛЕННО применяем изменения к компонентам
	switch key {
    case "provider":
        // Применяем только для текущей сессии
        ch.assistant.SetProvider(value, ch.assistant.GetModel(), ch.assistant.GetAPIKey())
        fmt.Printf("📊 Провайдер сессии: %s\n", value)
        
    case "model":
        ch.assistant.SetModel(value)
        fmt.Printf("📊 Модель сессии: %s\n", value)

	case "context_limit":
		if limit, err := strconv.Atoi(value); err == nil {
			ch.assistant.GetContext().SetMaxLength(limit)
			fmt.Printf("📊 Контекст обновлён: новый лимит %d обменов\n", limit)
		}
	case "debug_mode":
		fmt.Printf("🔧 Режим отладки: %v\n", ch.config.GetBool("debug_mode"))
	case "auto_execute":
		fmt.Printf("⚡ Auto-execute: %v (будет применено к новым запросам)\n", ch.config.GetBool("auto_execute"))
	case "max_retries":
		fmt.Printf("🔄 Max retries: %v (будет применено к новым запросам)\n", ch.config.GetInt("max_retries", 10))
	}

	// Сохраняем конфигурацию на диск
	if err := ch.config.Save(); err != nil {
		fmt.Printf("⚠️  Значение установлено, но не удалось сохранить: %v\n", err)
	} else {
		fmt.Printf("✅ %s = %s (сохранено)\n", key, value)
	}
}

func (ch *CommandHandler) handleGet(args []string) {
	if len(args) == 0 {
		fmt.Println("⚙️  Текущие настройки:")
		fmt.Println("  Ключ              Значение    Описание")
		fmt.Println("  ------------------------------------------------")
		
		settings := []struct{
			key string
			desc string
		}{
			{"debug_mode", "Режим отладки (вывод дополнительной информации)"},
			{"context_limit", "Максимальное количество обменов в контексте"},
			{"auto_execute", "Автоматическое выполнение сгенерированного кода"},
			{"max_retries", "Количество попыток запуска кода при ошибках"},
			{"web_search", "Включение поиска в интернете"},
            {"skip_install", "Режим пропуска автоматической установки зависимостей"},
		}
		
		for _, s := range settings {
			val := ch.config.GetAll()[s.key]
			fmt.Printf("  %-17s %-11v %s\n", s.key, val, s.desc)
		}
		return
	}

	key := args[0]
	if val, ok := ch.config.Get(key); ok {
		fmt.Printf("%s = %v\n", key, val)
	} else {
		fmt.Printf("❌ Настройка не найдена: %s\n", key)
		fmt.Println("Доступные настройки: debug_mode, context_limit, auto_execute, max_retries, web_search, skip_install")
	}
}

func (ch *CommandHandler) handleProviders() {
	ShowAvailableProviders()
}

func (ch *CommandHandler) handleProvider(args []string) {
	if len(args) == 0 {
		fmt.Printf("📊 Текущий провайдер: %s\n", ch.assistant.GetProvider())
		fmt.Printf("   Текущая модель: %s\n", ch.assistant.GetModel())
		if ch.assistant.GetAPIKey() != "" {
			fmt.Printf("   API ключ: установлен\n")
		} else {
			fmt.Printf("   API ключ: не установлен\n")
		}
		return
	}

	newProvider := args[0]
	
	// Проверка поддержки провайдера
	if !IsSupportedProvider(newProvider) {
		fmt.Printf("❌ Неподдерживаемый провайдер: %s\n", newProvider)
		fmt.Println("   Используйте :providers для списка поддерживаемых")
		fmt.Println("   Или укажите прямой URL: https://api.example.com")
		return
	}

	// Обработка URL-провайдера
	if isURLLLM(newProvider) {
		if len(args) < 2 {
			fmt.Println("❌ Для URL-провайдера укажите модель:")
			fmt.Println("   Использование: :provider <url> <model> [api_key]")
			fmt.Println("   Пример: :provider https://api.openai.com gpt-4 mykey123")
			return
		}
		url := newProvider
		model := args[1]
		apiKey := ""
		if len(args) > 2 {
			apiKey = args[2]
		}
		ch.assistant.SetProvider(url, model, apiKey)
		fmt.Printf("✅ Провайдер изменен (URL): %s\n", url)
		fmt.Printf("   Модель: %s\n", model)
		if apiKey != "" {
			fmt.Printf("   API ключ: установлен\n")
		}
		return
	}

	// Обработка именованного провайдера (ollama, openrouter и т.д.)
	oldProvider := ch.assistant.GetProvider()
	oldModel := ch.assistant.GetModel()
	
	// Для именованных провайдеров можно использовать текущую модель или указать новую
	newModel := ch.assistant.GetModel()
	if len(args) > 1 {
		newModel = args[1]
	}
	
	// Сохраняем текущий API ключ (для провайдеров, где он нужен)
	ch.assistant.SetProvider(newProvider, newModel, ch.assistant.GetAPIKey())
	fmt.Printf("✅ Провайдер изменен: %s → %s (только для текущей сессии)\n", 
		oldProvider, newProvider)
	if newModel != oldModel {
		fmt.Printf("   Модель: %s → %s\n", oldModel, newModel)
	}
}



// ========== Методы помощи ==========

func (ch *CommandHandler) showHelp(args []string) {
	if len(args) > 0 {
		cmd := args[0]
		if help, exists := commandHelp[cmd]; exists {
			fmt.Printf("ℹ️  Справка по команде %s:\n\n%s\n", cmd, help)
		} else {
			fmt.Printf("❌ Команда не найдена: %s\n", cmd)
			fmt.Println("Введите :help для списка всех команд")
		}
		return
	}

	fmt.Println("🤖 Доступные команды:")
	fmt.Println()
	fmt.Println("Контекст:")
	fmt.Println("  :clean              — Очистить историю")
	fmt.Println("  :pop [n]            — Удалить последние n обменов")
	fmt.Println("  :ctx                — Показать статистику контекста")
	fmt.Println("  :limit <число>      — Установить лимит контекста")
	fmt.Println("  :summarize          — Сжать контекст до сводки")
	fmt.Println()
    fmt.Println("Данные (RAG):")
    fmt.Println("  :data [путь]       — Загрузить файлы данных для RAG-режима")
	fmt.Println()
    fmt.Println("Буфер обмена:")
    fmt.Println("  :clip               — Показать буфер обмена")
    fmt.Println("  :clip+              — Добавить буфер в запрос")
    fmt.Println("  :copy [on|off]     — Вкл/выкл авто-копирование ответов")
    fmt.Println()
	fmt.Println("Сессии:")
	fmt.Println("  :save [имя]         — Сохранить сессию")
	fmt.Println("  :load <имя>         — Загрузить сессию")
	fmt.Println("  :ls                 — Список сессий")
	fmt.Println("  :rm <имя>           — Удалить сессию")
	fmt.Println("  :export [fmt]       — Экспортировать диалог")
	fmt.Println("  :history            — История последних команд")
	fmt.Println()
	fmt.Println("I/O:")
	fmt.Println("  :sh                 — Выполнить shell-команду в терминале")
	fmt.Println("  :cd <path>          — Сменить директорию")
	fmt.Println("  :pwd                — Текущая директория")
	fmt.Println("  :dir                — Посмотреть содержимое директории")
	fmt.Println("  :open <file>        — Открыть файл в редакторе")
	fmt.Println()
	fmt.Println("Отладка:")
    fmt.Println("  :skip  [on|off]     — Вкл/выкл пропуск установки")
	fmt.Println("  :debug [on|off]     — Включить/выключить дебаг")
	fmt.Println("  :stats              — Показать статистику")
	fmt.Println("  :retry              — Повторить последний запрос")
    fmt.Println("  :providers          — Показать список провайдеров")
    fmt.Println("  :provider <name>    — Изменить провайдера для сессии")
	fmt.Println("  :models             — Список моделей")
	fmt.Println("  :model <name>       — Изменить модель для сессии")
	fmt.Println()
	fmt.Println("Система:")
	fmt.Println("  :get [key]          — Показать настройки")
	fmt.Println("  :set <key> <value>  — Изменить настройку")
	fmt.Println("  :reset              — Сбросить настройки")
	fmt.Println("  :quit, :q           — Выход")
	fmt.Println("  :help, :h           — Эта справка")
	fmt.Println()
	fmt.Println("Для детальной справки по команде: :help <команда>")
}

