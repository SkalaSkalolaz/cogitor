// utils.go
// Вспомогательные функции и утилиты

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"os/exec"
)

// LogColor для цветного вывода (если терминал поддерживает)
func LogColor(color, message string) {
	if runtime.GOOS != "windows" {
		fmt.Printf("\033[%sm%s\033[0m\n", color, message)
	} else {
		fmt.Println(message)
	}
}

// GetLanguageByExtension возвращает язык программирования по расширению файла
func GetLanguageByExtension(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	langMap := map[string]string{
		".go":    "go",
		".py":    "python",
		".c":     "c",
		".cpp":   "cpp",
		".cc":    "cpp",
		".cxx":   "cpp",
		".f90":   "fortran",
		".f95":   "fortran",
		".f":     "fortran",
		".rb":    "ruby",
		".kt":    "kotlin",
		".swift": "swift",
		".html":  "html",
		".lisp":  "lisp",
		".cl":    "lisp",
		".asm":   "assembly",
		".s":     "assembly",
	}
	return langMap[ext]
}

// EnsureDir создает директорию, если она не существует
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// SplitPath разделяет путь на директорию и имя файла
func SplitPath(path string) (dir, file string) {
	dir = filepath.Dir(path)
	file = filepath.Base(path)
	return
}

// IsBinaryFile проверяет, является ли файл бинарным (упрощенная проверка)
func IsBinaryFile(path string) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	// Проверяем наличие нулевых байтов
	for _, b := range content {
		if b == 0 {
			return true
		}
	}
	return false
}

// OpenURLInBrowser открывает URL в системном браузере (Linux/macOS/Windows)
func OpenURLInBrowser(url string) error {
	var cmd *exec.Cmd
	
	switch runtime.GOOS {
	case "darwin": // macOS
		cmd = exec.Command("open", url)
	case "windows": // Windows
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default: // Linux и другие UNIX
		// Пробуем разные команды в порядке приоритета
		if _, err := exec.LookPath("xdg-open"); err == nil {
			cmd = exec.Command("xdg-open", url)
		} else if _, err := exec.LookPath("gnome-open"); err == nil {
			cmd = exec.Command("gnome-open", url)
		} else if _, err := exec.LookPath("kde-open"); err == nil {
			cmd = exec.Command("kde-open", url)
		} else {
			return fmt.Errorf("не найдены команды для открытия браузера (xdg-open, gnome-open или kde-open)")
		}
	}
	
	return cmd.Start() // Start() не ждет закрытия браузера
}