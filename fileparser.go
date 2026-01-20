// fileparser.go
// Парсинг и чтение файловых ссылок (@file, @all, $clip, и т.д.)

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"net/http"
	"time"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

// FileReference представляет ссылку на файл
type FileReference struct {
	Path      string
	LineStart int
	LineEnd   int
	IsAll     bool
	IsAbs     bool
	IsURL     bool
}

// FileParser парсит ссылки на файлы
type FileParser struct{}

// NewFileParser создает новый парсер файлов
func NewFileParser() *FileParser {
	return &FileParser{}
}

// ExtractFileReferences извлекает все ссылки на файлы из запроса
func (fp *FileParser) ExtractFileReferences(query string) ([]FileReference, bool) {
	var refs []FileReference
	hasRefs := false

	// Поиск ссылок @filename
	words := strings.Fields(query)
	for _, word := range words {
		if strings.HasPrefix(word, "@") {
			ref := fp.parseFileReference(word)
			if ref != nil {
				refs = append(refs, *ref)
				hasRefs = true
			}
		}
	}

	// Поиск $clip отдельно обрабатывается в assistant.go
	return refs, hasRefs
}

// parseFileReference парсит одну ссылку на файл
func (fp *FileParser) parseFileReference(ref string) *FileReference {
	if ref == "@all" {
		return &FileReference{IsAll: true}
	}

	path := strings.TrimPrefix(ref, "@")
	if path == "" {
		return nil
	}

	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
    	return &FileReference{
    		Path:  path,
    		IsURL: true,
    	}
    }


	// Проверка на абсолютный путь
	isAbs := strings.HasPrefix(path, "~/") || filepath.IsAbs(path)

	// Парсинг строк и диапазонов строк
	lineStart := 0
	lineEnd := 0

	parts := strings.Split(path, ":")
	if len(parts) > 1 {
		path = parts[0]
		rangeStr := parts[1]
		
		if strings.Contains(rangeStr, "-") {
			// Диапазон строк
			rangeParts := strings.Split(rangeStr, "-")
			if len(rangeParts) == 2 {
				fmt.Sscanf(rangeParts[0], "%d", &lineStart)
				fmt.Sscanf(rangeParts[1], "%d", &lineEnd)
			}
		} else {
			// Одна строка
			fmt.Sscanf(rangeStr, "%d", &lineStart)
			lineEnd = lineStart
		}
	}

	return &FileReference{
		Path:      path,
		LineStart: lineStart,
		LineEnd:   lineEnd,
		IsAbs:     isAbs,
	}
}

// ReadReferencedFiles читает все указанные файлы и возвращает контекст
func (fp *FileParser) ReadReferencedFiles(refs []FileReference) string {
	var context strings.Builder

	for _, ref := range refs {
		if ref.IsAll {
			context.WriteString(fp.readAllFiles())
		} else {
			context.WriteString(fp.readSingleFile(ref))
		}
	}

	return context.String()
}

// readAllFiles читает все файлы в текущей директории
// readAllFiles читает все файлы в текущей директории
func (fp *FileParser) readAllFiles() string {
	var context strings.Builder
	context.WriteString("Содержимое всех файлов проекта:\n")

	// Срез для сбора ошибок (для вывода в конце)
	var errors []string

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Фиксируем ошибку доступа и продолжаем обход
			errors = append(errors, fmt.Sprintf("⚠️ Ошибка доступа к %s: %v", path, err))
			return nil // продолжаем обработку других файлов
		}

		// Пропускаем директории и скрытые файлы
		if info.IsDir() || strings.HasPrefix(info.Name(), ".") {
			return nil
		}

		// Пропускаем бинарные файлы и не кодовые файлы
		if !fp.isSourceFile(path) {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			// Фиксируем ошибку чтения файла и продолжаем
			errors = append(errors, fmt.Sprintf("⚠️ Ошибка чтения файла %s: %v", path, err))
			return nil // продолжаем обход
		}

		context.WriteString(fmt.Sprintf("\n--- File: %s ---\n", path))
		context.WriteString(string(content))

		return nil
	})

	if err != nil {
		// Это критическая ошибка самого Walk (например, отказ корневой директории)
		// Добавляем её в начало списка ошибок
		errors = append([]string{fmt.Sprintf("⚠️ Ошибка обхода директорий: %v", err)}, errors...)
	}

	// Добавляем секцию с ошибками в контекст, если они есть
	if len(errors) > 0 {
		context.WriteString("\n--- Ошибки при чтении файлов ---\n")
		for _, errMsg := range errors {
			context.WriteString(errMsg + "\n")
		}
		context.WriteString("---------------------------------\n")
	}

	return context.String()
}

// readSingleFile читает один файл с возможностью указания строк
func (fp *FileParser) readSingleFile(ref FileReference) string {
	path := ref.Path

	// Обработка домашней директории
    if strings.HasPrefix(path, "~/") {
    	home, _ := os.UserHomeDir()
    	path = filepath.Join(home, path[2:])
    }
    
    // Проверяем, что файл не выходит за пределы рабочей директории
    if !ref.IsAbs {
    	safePath, err := fp.resolveSafePath(path)
    	if err != nil {
    		return fmt.Sprintf("--- File: %s ---\n⚠️ Ошибка валидации пути: %v\n", path, err)
    	}
    	path = safePath
    }
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("--- File: %s ---\n⚠️ Ошибка чтения: %v\n", path, err)
	}

	result := fmt.Sprintf("\n--- File: %s ---\n", path)

	// Если указаны строки, выводим только их
	if ref.LineStart > 0 {
		lines := strings.Split(string(content), "\n")
		if ref.LineEnd == 0 {
			ref.LineEnd = ref.LineStart
		}

		start := ref.LineStart - 1
		end := ref.LineEnd

		if start >= len(lines) {
			return fmt.Sprintf("⚠️ Файл %s содержит только %d строк\n", path, len(lines))
		}

		if end > len(lines) {
			end = len(lines)
		}

		for i := start; i < end; i++ {
			result += fmt.Sprintf("%d: %s\n", i+1, lines[i])
		}
	} else {
		result += string(content)
	}

	return result
}

// isSourceFile определяет, является ли файл исходным кодом
func (fp *FileParser) isSourceFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	sourceExts := []string{
		".go", ".py", ".c", ".cpp", ".cc", ".cxx", ".h", ".hpp",
		".f", ".f90", ".f95", ".rb", ".kt", ".swift", ".html",
		".lisp", ".cl", ".asm", ".s", ".txt",
	}

	for _, se := range sourceExts {
		if ext == se {
			return true
		}
	}
	return false
}

// func normalizeWhitespace(s string) string {
	// return strings.Join(strings.Fields(s), " ")
// }
// 
// FetchURLContent загружает и извлекает текст из веб-страницы
func (fp *FileParser) FetchURLContent(urlStr string) (string, error) {
	client := &http.Client{
		Timeout: 20 * time.Second,
	}
	
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return "", fmt.Errorf("ошибка создания запроса: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; AI-Assistant/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ошибка загрузки страницы: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP статус: %d", resp.StatusCode)
	}

	reader, err := charset.NewReader(resp.Body, resp.Header.Get("Content-Type"))
	if err != nil {
		return "", err
	}
	
	doc, err := html.Parse(reader)
	if err != nil {
		return "", fmt.Errorf("ошибка парсинга HTML: %w", err)
	}

	// Используем существующие функции из websearch.go
	text := extractText(doc)

	text = normalizeWhitespace(text)
	
	// Ограничение размера
	const maxLength = 50000
	if len(text) > maxLength {
		text = text[:maxLength] + "\n... (обрезано)"
	}
	
	return text, nil
}

// resolveSafePath проверяет, что относительный путь не содержит попыток выйти за пределы рабочей директории
func (fp *FileParser) resolveSafePath(relativePath string) (string, error) {
	// Очищаем путь
	cleanPath := filepath.Clean(relativePath)
	
	// Запрещаем попытки выйти вверх по иерархии
	if strings.Contains(cleanPath, ".."+string(filepath.Separator)) || cleanPath == ".." {
		return "", fmt.Errorf("путь содержит недопустимые элементы: %s", relativePath)
	}
	
	// Получаем абсолютный путь рабочей директории
	workingDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("не удалось получить текущую директорию: %v", err)
	}
	
	// Собираем полный путь
	fullPath := filepath.Join(workingDir, cleanPath)
	
	// Проверяем, что результат не выходит за рабочую директорию
	absWorkingDir, err := filepath.Abs(workingDir)
	if err != nil {
		return "", err
	}
	
	absFullPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", err
	}
	
	// Дополнительная проверка через filepath.Rel
	relPath, err := filepath.Rel(absWorkingDir, absFullPath)
	if err != nil {
		return "", fmt.Errorf("не удалось проверить относительный путь: %v", err)
	}
	
	if strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("путь выходит за пределы рабочей директории: %s", fullPath)
	}
	
	return fullPath, nil
}