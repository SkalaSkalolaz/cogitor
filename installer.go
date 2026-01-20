// installer.go
// Проверка и установка зависимостей для разных языков программирования

package main

import (
	// "bufio"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"path/filepath" 
)

// Installer управляет установкой зависимостей
type Installer struct {
	terminalReader *TerminalReader
	config         *Config
}

// NewInstaller создает новый инсталлятор
func NewInstaller(tr *TerminalReader, config *Config) *Installer {
	return &Installer{
		terminalReader: tr,
		config:         config,
	}
}

// DependencyInfo содержит информацию о зависимости
type DependencyInfo struct {
	Language   string
	Package    string
	InstallCmd string
	IsFound    bool
}

// CheckAndInstallDependencies проверяет и предлагает установить зависимости
func (i *Installer) CheckAndInstallDependencies(files []CodeFile) error {
	// Сначала проверяем, есть ли команды установки от LLM
	var llmCommands []string
	for _, file := range files {
		if file.Compile != nil && file.Compile.InstallCommand != "" {
			llmCommands = append(llmCommands, file.Compile.InstallCommand)
		}
	}
	
	// Если есть команды от LLM, выполняем их в приоритете
	if len(llmCommands) > 0 {
		fmt.Println("\n🔍 Обнаружены команды установки от LLM:")
		for _, cmd := range llmCommands {
			fmt.Printf("  - %s\n", cmd)
		}
		
		if i.config.GetBool("skip_install") {
			fmt.Println("\n⚠️  Режим пропуска установки активен")
			for _, cmd := range llmCommands {
				fmt.Printf("\n📋 Установите зависимость вручную:\n   %s\n", cmd)
				fmt.Println("   После установки нажмите Enter...")
				i.waitForEnter()
			}
			fmt.Println("✅ Продолжение работы...")
			return nil
		}

		response, err := i.terminalReader.ReadLineWithPrompt("Установить зависимости? (y/n): ")
		if err != nil {
			fmt.Printf("❌ Ошибка ввода: %v\n", err)
			return nil
		}
		
		if strings.ToLower(strings.TrimSpace(response)) == "y" {
			for _, cmd := range llmCommands {
				fmt.Printf("\n💡 Установка: %s\n", cmd)
				if err := i.ExecuteInstallCommand(cmd); err != nil {
					return fmt.Errorf("ошибка установки: %v", err)
				}
			}
		}
		return nil
	}
	
	// Fallback: автоматический анализ кода (старая логика)
	deps := i.analyzeDependencies(files)
	if len(deps) == 0 {
		return nil
	}

	fmt.Println("\n🔍 Обнаружены потенциальные зависимости:")
	for _, dep := range deps {
		if !dep.IsFound {
			fmt.Printf("  - %s (%s)\n", dep.Package, dep.Language)
		}
	}
	fmt.Println()
		if i.config.GetBool("skip_install") {
		fmt.Println("⚠️  Режим пропуска установки активен")
		for _, dep := range deps {
			if !dep.IsFound {
				fmt.Printf("\n📋 Установите зависимость вручную:\n   %s\n", dep.InstallCmd)
				fmt.Println("   После установки нажмите Enter...")
				i.waitForEnter()
			}
		}
		fmt.Println("✅ Продолжение работы...")
		return nil
	}

    response, err := i.terminalReader.ReadLineWithPrompt("Установить зависимости? (y/n): ")
    if err != nil {
        fmt.Printf("❌ Ошибка ввода: %v\n", err)
        return nil
    }

	
	if strings.ToLower(strings.TrimSpace(response)) != "y" {
		fmt.Println("⏭️  Пропуск установки зависимостей")
		return nil
	}

	// Устанавливаем
	for _, dep := range deps {
		if !dep.IsFound {
			fmt.Printf("\n💡 Установка: %s\n", dep.InstallCmd)
			fmt.Printf("   Нажмите Enter для продолжения... ")
            _, _ = i.terminalReader.ReadLineWithPrompt("> ")

			cmd := exec.Command("sh", "-c", dep.InstallCmd)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("ошибка установки %s: %v", dep.Package, err)
			}
			
			fmt.Printf("✅ Установлено: %s\n", dep.Package)
		}
	}

	return nil
}

// waitForEnter ожидает нажатия Enter от пользователя
func (i *Installer) waitForEnter() {
	_, _ = i.terminalReader.ReadLineWithPrompt("> Нажмите Enter для продолжения...")
}

// ExecuteInstallCommand выполняет команду установки и выводит результат
func (i *Installer) ExecuteInstallCommand(command string) error {
	fmt.Printf("🚀 Выполнение: %s\n", command)
	
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return fmt.Errorf("пустая команда")
	}
	
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ошибка выполнения команды: %w", err)
	}
	
	return nil
}

// analyzeDependencies анализирует код на предмет зависимостей
func (i *Installer) analyzeDependencies(files []CodeFile) []DependencyInfo {
	var deps []DependencyInfo

	for _, file := range files {
		    // Пропускаем текстовые файлы
        if strings.HasSuffix(strings.ToLower(file.Path), ".txt") {
            continue
        }
		lang := i.detectLanguage(file.Path)
		if lang == "" {
			continue
		}

		// Анализируем импорты/включения
		lines := strings.Split(file.Content, "\n")
		
		switch lang {
		case "python":
			deps = append(deps, i.extractPythonDeps(lines)...)
		case "go":
			deps = append(deps, i.extractGoDeps(lines)...)
		case "ruby":
			deps = append(deps, i.extractRubyDeps(lines)...)
		case "cpp", "c":
			deps = append(deps, i.extractCppDeps(lines)...)
		}
	}

	return deps
}

// detectLanguage определяет язык по расширению файла
func (i *Installer) detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	langMap := map[string]string{
		".py": "python",
		".go": "go",
		".rb": "ruby",
		".cpp": "cpp",
		".c": "c",
		".kt": "kotlin",
	}
	return langMap[ext]
}

// extractPythonDeps извлекает зависимости Python
func (i *Installer) extractPythonDeps(lines []string) []DependencyInfo {
	var deps []DependencyInfo
	
	importRegex := regexp.MustCompile(`^\s*(?:import|from)\s+([a-zA-Z0-9_]+)`)
	
	for _, line := range lines {
		matches := importRegex.FindStringSubmatch(line)
		if len(matches) > 1 {
			pkg := matches[1]
			if i.isStandardLib("python", pkg) {
				continue
			}
			
			deps = append(deps, DependencyInfo{
				Language:   "python",
				Package:    pkg,
				InstallCmd: fmt.Sprintf("pip3 install %s", pkg),
				IsFound:    i.isPackageInstalled("python", pkg),
			})
		}
	}
	
	return deps
}

// extractGoDeps извлекает зависимости Go
func (i *Installer) extractGoDeps(lines []string) []DependencyInfo {
	var deps []DependencyInfo
	
	importRegex := regexp.MustCompile(`^\s*([a-zA-Z0-9_]+\s+)?"([^"]+)"`)
	
	for _, line := range lines {
		matches := importRegex.FindStringSubmatch(line)
		if len(matches) > 2 {
			pkg := matches[2]
			if strings.Contains(pkg, ".") && !strings.Contains(pkg, "internal") {
				deps = append(deps, DependencyInfo{
					Language:   "go",
					Package:    pkg,
					InstallCmd: fmt.Sprintf("go get %s", pkg),
					IsFound:    i.isPackageInstalled("go", pkg),
				})
			}
		}
	}
	
	return deps
}

// extractRubyDeps извлекает зависимости Ruby
func (i *Installer) extractRubyDeps(lines []string) []DependencyInfo {
	var deps []DependencyInfo
	
	requireRegex := regexp.MustCompile(`^\s*require\s+['"]([^'"]+)['"]`)
	
	for _, line := range lines {
		matches := requireRegex.FindStringSubmatch(line)
		if len(matches) > 1 {
			pkg := matches[1]
			if !strings.Contains(pkg, ".") {
				continue
			}
			
			deps = append(deps, DependencyInfo{
				Language:   "ruby",
				Package:    pkg,
				InstallCmd: fmt.Sprintf("gem install %s", pkg),
				IsFound:    i.isPackageInstalled("ruby", pkg),
			})
		}
	}
	
	return deps
}

// extractCppDeps извлекает зависимости C/C++
func (i *Installer) extractCppDeps(lines []string) []DependencyInfo {
	var deps []DependencyInfo
	
	includeRegex := regexp.MustCompile(`^\s*#include\s+<([^>]+)>`)
	
	for _, line := range lines {
		matches := includeRegex.FindStringSubmatch(line)
		if len(matches) > 1 {
			header := matches[1]
			deps = append(deps, DependencyInfo{
				Language: "cpp",
				Package:  header,
				InstallCmd: i.getCppPackageInstallCmd(header),
				IsFound:  i.isCppHeaderInstalled(header),
			})
		}
	}
	
	return deps
}

// isStandardLib проверяет, является ли пакет стандартной библиотекой
func (i *Installer) isStandardLib(language, pkg string) bool {
	standardLibs := map[string][]string{
		"python": {"os", "sys", "math", "json", "time", "datetime", "re", "collections", "itertools", "functools"},
		"go":     {"fmt", "os", "io", "net", "http", "time", "strings", "strconv"},
	}
	
	libs, exists := standardLibs[language]
	if !exists {
		return false
	}
	
	for _, lib := range libs {
		if lib == pkg {
			return true
		}
	}
	return false
}

// isPackageInstalled проверяет, установлен ли пакет
func (i *Installer) isPackageInstalled(language, pkg string) bool {
	var cmd *exec.Cmd
	
	switch language {
	case "python":
		cmd = exec.Command("python3", "-c", fmt.Sprintf("import %s", pkg))
	case "go":
		cmd = exec.Command("go", "list", pkg)
	case "ruby":
		cmd = exec.Command("ruby", "-e", fmt.Sprintf("require '%s'", pkg))
	case "cpp":
		return i.isCppHeaderInstalled(pkg)
	case "asm":
        _, err := exec.LookPath("nasm")
        return err == nil
	default:
		return false
	}
	
	err := cmd.Run()
	return err == nil
}

// isCppHeaderInstalled проверяет, установлен ли заголовочный файл C/C++
func (i *Installer) isCppHeaderInstalled(header string) bool {
	// Это упрощенная проверка, на практике нужно проверять пути
	commonPaths := []string{
		"/usr/include",
		"/usr/local/include",
	}
	
	for _, basePath := range commonPaths {
		if _, err := os.Stat(filepath.Join(basePath, header)); err == nil {
			return true
		}
	}
	return false
}

// getCppPackageInstallCmd возвращает команду установки пакета C/C++
func (i *Installer) getCppPackageInstallCmd(header string) string {
	// Сопоставление заголовков с пакетами (упрощенное)
	packageMap := map[string]string{
		"boost/":       "libboost-all-dev",
		"SDL.h":        "libsdl2-dev",
		"gtk/gtk.h":    "libgtk-3-dev",
		"curl/curl.h":  "libcurl4-openssl-dev",
		"json/json.h":  "libjsoncpp-dev",
		"asm/asm.h": 	"nasm",
	}
	
	for key, pkg := range packageMap {
		if strings.Contains(header, key) {
			return fmt.Sprintf("sudo apt-get install %s", pkg)
		}
	}
	
	return fmt.Sprintf("# Установите пакет, содержащий %s", header)
}