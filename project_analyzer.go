// project_analyzer.go
// Анализ структуры проекта и определение команды запуска

package main

import (
	"fmt"
	// "os"
	"path/filepath"
	"sort"
	"strings"
)

// ProjectConfig содержит конфигурацию проекта
type ProjectConfig struct {
	Language       string
	EntryPoint     string
	Files          []string
	CompileCommand string
	RunCommand     string
	Args           []string
	HasMakefile    bool
	HasGoMod       bool
	HasPyMain      string // Путь к __main__.py если есть
}

// ProjectAnalyzer анализирует сгенерированный код
type ProjectAnalyzer struct {
	codeFiles []CodeFile
}

// NewProjectAnalyzer создает анализатор
func NewProjectAnalyzer(codeFiles []CodeFile) *ProjectAnalyzer {
	return &ProjectAnalyzer{codeFiles: codeFiles}
}

// Analyze анализирует проект и возвращает конфигурацию запуска
func (pa *ProjectAnalyzer) Analyze() *ProjectConfig {
	config := &ProjectConfig{
		Files: make([]string, 0, len(pa.codeFiles)),
	}
	
	// Собираем все пути
	for _, f := range pa.codeFiles {
		config.Files = append(config.Files, f.Path)
	}
	
	// Определяем язык проекта
	config.Language = pa.detectMainLanguage()
	
	// Ищем точку входа
	config.EntryPoint = pa.findEntryPoint()
	
	// Проверяем наличие системных файлов
	config.HasMakefile = pa.hasFile("Makefile")
	config.HasGoMod = pa.hasFile("go.mod")
	config.HasPyMain = pa.findPyMainFile()
	
	// Формируем команды
	config.CompileCommand, config.RunCommand = pa.buildCommands(config)
	
	return config
}

// detectMainLanguage определяет основной язык проекта
func (pa *ProjectAnalyzer) detectMainLanguage() string {
	langCount := make(map[string]int)
	for _, f := range pa.codeFiles {
		lang := GetLanguageByExtension(f.Path)
		if lang != "" {
			langCount[lang]++
		}
	}
	
	maxCount := 0
	mainLang := ""
	for lang, count := range langCount {
		if count > maxCount {
			maxCount = count
			mainLang = lang
		}
	}
	return mainLang
}

// findEntryPoint ищет файл с точкой входа программы
func (pa *ProjectAnalyzer) findEntryPoint() string {
	rules := map[string][]string{
		"go":     {"main.go", "cmd/main.go", "src/main.go"},
		"c":      {"main.c", "src/main.c"},
		"cpp":    {"main.cpp", "main.cc", "src/main.cpp"},
		"python": {"main.py", "__main__.py", "app.py"},
		"ruby":   {"main.rb", "app.rb"},
		"fortran": {"main.f90", "program.f90"},
		"swift":  {"main.swift"},
		"kotlin": {"Main.kt", "main.kt"},
		"lisp":   {"main.lisp", "main.cl"},
		"assembly": {"main.asm", "main.s"},
	}
	
	mainLang := pa.detectMainLanguage()
	if ruleFiles, exists := rules[mainLang]; exists {
		for _, rule := range ruleFiles {
			if pa.hasFile(rule) {
				return rule
			}
		}
	}
	
	// Ищем файлы с main в имени
	for _, f := range pa.codeFiles {
		base := filepath.Base(f.Path)
		if strings.Contains(base, "main") {
			return f.Path
		}
	}
	
	// Возвращаем первый файл основного языка
	for _, f := range pa.codeFiles {
		if GetLanguageByExtension(f.Path) == mainLang {
			return f.Path
		}
	}
	
	return ""
}

// hasFile проверяет наличие файла
func (pa *ProjectAnalyzer) hasFile(filename string) bool {
	for _, f := range pa.codeFiles {
		if filepath.Base(f.Path) == filename {
			return true
		}
	}
	return false
}

// findPyMainFile ищет __main__.py
func (pa *ProjectAnalyzer) findPyMainFile() string {
	for _, f := range pa.codeFiles {
		if filepath.Base(f.Path) == "__main__.py" {
			return f.Path
		}
	}
	return ""
}

// buildCommands формирует команды компиляции и запуска
func (pa *ProjectAnalyzer) buildCommands(config *ProjectConfig) (compileCmd, runCmd string) {
	switch config.Language {
	case "go":
		if config.HasGoMod {
			compileCmd = "go build -o main ."
			runCmd = "./main"
		} else if config.EntryPoint != "" {
			compileCmd = "go build -o main " + config.EntryPoint
			runCmd = "./main"
		}
	case "c":
		files := pa.getFilesByExts(".c")
		if len(files) > 1 {
			compileCmd = fmt.Sprintf("gcc %s -o main", strings.Join(files, " "))
		} else if config.EntryPoint != "" {
			compileCmd = "gcc " + config.EntryPoint + " -o main"
		}
		runCmd = "./main"
	case "cpp":
		files := pa.getFilesByExts(".cpp", ".cc")
		if len(files) > 1 {
			compileCmd = fmt.Sprintf("g++ %s -o main", strings.Join(files, " "))
		} else if config.EntryPoint != "" {
			compileCmd = "g++ " + config.EntryPoint + " -o main"
		}
		runCmd = "./main"
	case "fortran":
		files := pa.getFilesByExts(".f90", ".f95", ".f")
		if len(files) > 1 {
			compileCmd = fmt.Sprintf("gfortran %s -o main", strings.Join(files, " "))
		} else if config.EntryPoint != "" {
			compileCmd = "gfortran " + config.EntryPoint + " -o main"
		}
		runCmd = "./main"
	case "assembly":
		files := pa.getFilesByExts(".asm")
		if len(files) > 1 {
			compileCmd = fmt.Sprintf("nasm -f elf64 %s -o objs.o && ld -o main objs.o", strings.Join(files, " -f elf64 "))
		} else if config.EntryPoint != "" {
			name := strings.TrimSuffix(config.EntryPoint, ".asm")
			compileCmd = "nasm -f elf64 " + config.EntryPoint + " -o " + name + ".o && ld -o " + name + " " + name + ".o"
		}
		runCmd = "./main"
	case "python":
		if config.HasPyMain != "" {
			dir := filepath.Dir(config.HasPyMain)
			runCmd = "python3 -m " + filepath.Base(dir)
		} else if config.EntryPoint != "" {
			runCmd = "python3 " + config.EntryPoint
		}
	case "ruby":
		if config.EntryPoint != "" {
			runCmd = "ruby " + config.EntryPoint
		}
	case "kotlin":
		if config.EntryPoint != "" {
			compileCmd = "kotlinc -include-runtime " + config.EntryPoint + " -d main.jar"
			runCmd = "java -jar main.jar"
		}
	case "swift":
		if config.EntryPoint != "" {
			compileCmd = "swiftc " + config.EntryPoint + " -o main"
			runCmd = "./main"
		}
	case "lisp":
		if config.EntryPoint != "" {
			runCmd = "sbcl --script " + config.EntryPoint
		}
	}
	return
}

// getFilesByExt возвращает файлы с указанными расширениями
func (pa *ProjectAnalyzer) getFilesByExts(exts ...string) []string {
	var files []string
	for _, f := range pa.codeFiles {
		for _, ext := range exts {
			if strings.HasSuffix(strings.ToLower(f.Path), ext) {
				files = append(files, f.Path)
				break
			}
		}
	}
	return files
}

// GetAvailableEntryPoints возвращает все возможные точки входа
func (pa *ProjectAnalyzer) GetAvailableEntryPoints() []string {
	set := make(map[string]bool)
	
	if ep := pa.findEntryPoint(); ep != "" {
		set[ep] = true
	}
	
	for _, f := range pa.codeFiles {
		base := filepath.Base(f.Path)
		if strings.Contains(base, "main") {
			set[f.Path] = true
		}
	}
	
	if pyMain := pa.findPyMainFile(); pyMain != "" {
		set[pyMain] = true
	}
	
	result := make([]string, 0, len(set))
	for path := range set {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}