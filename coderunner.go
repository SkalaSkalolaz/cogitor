// coderunner.go
// Компиляция и запуск кода на разных языках, обработка ошибок с помощью LLM

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"context"
)

// CodeRunner управляет компиляцией и запуском кода
type CodeRunner struct {
	maxRetries int
}

// NewCodeRunner создает новый раннер кода
func NewCodeRunner(config *Config) *CodeRunner {
	maxRetries := 10
	if config != nil {
		maxRetries = config.GetInt("max_retries", 10)
	}
	return &CodeRunner{
		maxRetries: maxRetries,
	}
}

// LanguageInfo содержит информацию о языке программирования
type LanguageInfo struct {
	Extension string
	Compiler  string
	Runner    string
	NeedsCompile bool
}

// buildCompileCommand строит команду компиляции с учетом CompileInfo
func (cr *CodeRunner) buildCompileCommand(file string, langInfo *LanguageInfo, compileInfo *CompileInfo) (*exec.Cmd, string, error) {
	dir := filepath.Dir(file)
	filename := filepath.Base(file)
	nameWithoutExt := strings.TrimSuffix(filename, filepath.Ext(filename))

	// Меняем директорию на директорию файла
	originalDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(originalDir)

	var cmd *exec.Cmd
	var outputFile string

	// Если задана полная команда - используем её
	if compileInfo != nil && compileInfo.Command != "" {
		// Парсим команду на части
		parts := strings.Fields(compileInfo.Command)
		if len(parts) > 0 {
			cmd = exec.Command(parts[0], parts[1:]...)
			// Для команд типа "gcc -o output file.c -lssl" outputFile будет "output"
			if len(parts) > 2 && parts[1] == "-o" {
				outputFile = parts[2]
			}
			return cmd, outputFile, nil
		}
	}

	// Если заданы только флаги - добавляем их к стандартной команде
	flags := ""
	if compileInfo != nil && compileInfo.Flags != "" {
		flags = " " + compileInfo.Flags
	}

	if langInfo.NeedsCompile {
		// Компиляция
		switch langInfo.Extension {
		case ".c", ".cpp", ".cc":
			outputFile = nameWithoutExt
			if runtime.GOOS == "windows" {
				outputFile += ".exe"
			}
			compileCmd := langInfo.Compiler + flags + " -o " + outputFile + " " + filename
			parts := strings.Fields(compileCmd)
			cmd = exec.Command(parts[0], parts[1:]...)
		case ".f90", ".f95":
			outputFile = nameWithoutExt
			compileCmd := langInfo.Compiler + flags + " -o " + outputFile + " " + filename
			parts := strings.Fields(compileCmd)
			cmd = exec.Command(parts[0], parts[1:]...)
		case ".kt":
			if _, err := exec.LookPath("java"); err != nil {
				return nil, "", fmt.Errorf("java не найден в PATH. Установите JDK для запуска Kotlin")
			}
			jarFile := nameWithoutExt + ".jar"
			compileCmd := langInfo.Compiler + flags + " -include-runtime -d " + jarFile + " " + filename
			parts := strings.Fields(compileCmd)
			compileCmdObj := exec.Command(parts[0], parts[1:]...)
			if output, err := compileCmdObj.CombinedOutput(); err != nil {
				return nil, "", fmt.Errorf("%s", string(output))
			}
			outputFile = jarFile
			// Для Kotlin команда запуска отличается от компиляции
			return exec.Command("java", "-jar", jarFile), outputFile, nil
		case ".swift":
			outputFile = nameWithoutExt
			compileCmd := langInfo.Compiler + flags + " -o " + outputFile + " " + filename
			parts := strings.Fields(compileCmd)
			cmd = exec.Command(parts[0], parts[1:]...)
		case ".asm":
			objectFile := nameWithoutExt + ".o"
			executableFile := nameWithoutExt
			
			// Первый этап: компиляция в объектный файл
			compileCmd := "nasm" + flags + " -f elf64 " + filename + " -o " + objectFile
			parts := strings.Fields(compileCmd)
			compileCmdObj := exec.Command(parts[0], parts[1:]...)
			if output, err := compileCmdObj.CombinedOutput(); err != nil {
				os.Remove(objectFile)
				return nil, "", fmt.Errorf("%s", string(output))
			}
			
			// Второй этап: линковка
			linkCmd := "ld -o " + executableFile + " " + objectFile
			parts = strings.Fields(linkCmd)
			linkCmdObj := exec.Command(parts[0], parts[1:]...)
			if output, err := linkCmdObj.CombinedOutput(); err != nil {
				os.Remove(objectFile)
				os.Remove(executableFile)
				return nil, "", fmt.Errorf("%s", string(output))
			}
			
			os.Remove(objectFile)
			os.Chmod(executableFile, 0755)
			outputFile = executableFile
			// Команда уже выполнена
			return nil, outputFile, nil
		case ".s":
			outputFile = nameWithoutExt
			compileCmd := langInfo.Compiler + flags + " -o " + outputFile + " " + filename
			parts := strings.Fields(compileCmd)
			cmd = exec.Command(parts[0], parts[1:]...)
		}

		if cmd != nil {
			return cmd, outputFile, nil
		}

		// Запуск
		if outputFile != "" {
			return exec.Command("./" + outputFile), outputFile, nil
		}
	} else {
		// Интерпретатор или прямой запуск
		switch langInfo.Extension {
		case ".go":
			runCmd := "go run " + flags + " " + filename
			parts := strings.Fields(runCmd)
			cmd = exec.Command(parts[0], parts[1:]...)
		case ".py":
			// Для Python флаги могут быть переменными окружения
			if flags != "" {
				// Парсим формат KEY=VALUE
				envVars := strings.Split(flags, " ")
				cmd = exec.Command("python3", filename)
				cmd.Env = append(os.Environ(), envVars...)
			} else {
				cmd = exec.Command("python3", filename)
			}
		case ".rb":
			cmd = exec.Command("ruby", filename)
		case ".lisp", ".cl":
			cmd = exec.Command("sbcl", "--script", filename)
		case ".html":
			// Открываем HTML файл в браузере
			absPath, err := filepath.Abs(file)
			if err != nil {
				return nil, "", fmt.Errorf("не удалось получить абсолютный путь: %v", err)
			}
			fileURL := "file://" + absPath
			if runtime.GOOS == "windows" {
				fileURL = "file:///" + strings.ReplaceAll(absPath, "\\", "/")
			}
			if err := OpenURLInBrowser(fileURL); err != nil {
				return nil, "", fmt.Errorf("не удалось открыть HTML в браузере: %v", err)
			}
			return nil, "", nil // Специальный случай
		default:
			runCmd := langInfo.Runner + flags + " " + filename
			parts := strings.Fields(runCmd)
			cmd = exec.Command(parts[0], parts[1:]...)
		}
	}

	return cmd, outputFile, nil
}

// getLanguageInfo возвращает информацию о языке по расширению файла
func (cr *CodeRunner) getLanguageInfo(filepath string) *LanguageInfo {
	ext := strings.ToLower(filepath)
	
	langMap := map[string]*LanguageInfo{
		".go":    {Extension: ".go", Compiler: "go", Runner: "go run", NeedsCompile: false},
		".py":    {Extension: ".py", Compiler: "python3", Runner: "python3", NeedsCompile: false},
		".c":     {Extension: ".c", Compiler: "gcc", Runner: "./", NeedsCompile: true},
		".cpp":   {Extension: ".cpp", Compiler: "g++", Runner: "./", NeedsCompile: true},
		".cc":    {Extension: ".cc", Compiler: "g++", Runner: "./", NeedsCompile: true},
		".f90":   {Extension: ".f90", Compiler: "gfortran", Runner: "./", NeedsCompile: true},
		".f95":   {Extension: ".f95", Compiler: "gfortran", Runner: "./", NeedsCompile: true},
		".rb":    {Extension: ".rb", Compiler: "ruby", Runner: "ruby", NeedsCompile: false},
		".kt":    {Extension: ".kt", Compiler: "kotlinc", Runner: "java -jar", NeedsCompile: true},
		".swift": {Extension: ".swift", Compiler: "swiftc", Runner: "./", NeedsCompile: true},
		".html":  {Extension: ".html", Compiler: "", Runner: "browser", NeedsCompile: false},		
		".lisp":  {Extension: ".lisp", Compiler: "sbcl", Runner: "sbcl", NeedsCompile: false},
		".cl":    {Extension: ".cl", Compiler: "sbcl", Runner: "sbcl", NeedsCompile: false},
		".asm":   {Extension: ".asm", Compiler: "nasm", Runner: "./", NeedsCompile: true},
		".s":     {Extension: ".s", Compiler: "as", Runner: "./", NeedsCompile: true},
	}

	for extKey, info := range langMap {
		if strings.HasSuffix(ext, extKey) {
			return info
		}
	}
	return nil
}

// RunWithRetry запускает компиляцию и выполнение с повторными попытками
func (cr *CodeRunner) RunWithRetry(ctx context.Context, file string, originalCode, provider, model, apiKey string, compileInfo *CompileInfo) error {
	langInfo := cr.getLanguageInfo(file)
	if langInfo == nil {
		return fmt.Errorf("неподдерживаемый язык программирования: %s", file)
	}

	// Выполняем команду установки зависимостей, если она предоставлена
	if compileInfo != nil && compileInfo.InstallCommand != "" {
		fmt.Printf("📦 Установка зависимостей: %s\n", compileInfo.InstallCommand)
		installErr := cr.executeInstallCommand(ctx, compileInfo.InstallCommand)
		if installErr != nil {
			fmt.Printf("❌ Ошибка установки зависимостей: %v\n", installErr)
			// Запрашиваем подтверждение у пользователя
			reader := NewTerminalReader("🤖 Установка: ", 20)
			response, promptErr := reader.ReadLineWithPrompt("Установить зависимости вручную? (y/n): ")
			if promptErr != nil || strings.ToLower(strings.TrimSpace(response)) != "y" {
				return fmt.Errorf("установка зависимостей отменена пользователем")
			}
			// Показываем команду для ручной установки
			fmt.Printf("⚠️  Выполните команду вручную:\n   %s\n", compileInfo.InstallCommand)
			fmt.Println("   После установления зависимостей повторите запрос.")
			return fmt.Errorf("необходима ручная установка зависимостей")
		}
		fmt.Println("✅ Зависимости успешно установлены")
	}

	// Для HTML файлов не нужны повторные попытки, сразу открываем в браузере
	if langInfo.Extension == ".html" {
		_, err := cr.runCode(file, langInfo, compileInfo)
		return err
	}

	for attempt := 1; attempt <= cr.maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			fmt.Println("Запрос отменён пользователем")
			return fmt.Errorf("запрос отменён")
		default:
		}
		fmt.Printf("  Попытка %d/%d...\n", attempt, cr.maxRetries)

		output, err := cr.runCode(file, langInfo, compileInfo)

		if err == nil {
			fmt.Printf("✅ Выполнение успешно!\n")
			if output != "" {
				fmt.Printf("Вывод: %s\n", output)
			}
			return nil
		}

		// Ошибка - показываем пользователю
		fmt.Printf("❌ Ошибка: %v\n", err)
		if output != "" {
			fmt.Printf("Вывод компилятора/интерпрететора:\n%s\n", output)
		}

		if attempt < cr.maxRetries {
			fmt.Println("🤖 Пробую исправить код с помощью LLM...")
			
			// Формируем промпт для исправления
			filename := filepath.Base(file)
			var fixPrompt string
			
			if compileInfo != nil && (compileInfo.Flags != "" || compileInfo.Command != "") {
				// Указываем LLM, что были использованы специальные флаги
				fixPrompt = fmt.Sprintf(`Исправь следующий код (но не меняй его кардинально, а внеси точечные изменения), который вызвал ошибку при компиляции с флагами:

Файл: %s
Ошибка: %v
Вывод компилятора: %s
Использованные флаги: %v

ТЕКУЩИЙ КОД:
%s

ВЕРНИ ТОЛЬКО исправленный код в формате:
--- File: %s ---
<исправленный код здесь, без markdown>

Если нужны специфические флаги компиляции, добавь:
--- Compile: %s ---
<флаги или команда>

ВАЖНО: 
- НЕ используйте markdown 
- Код должен быть чистым и готовым к выполнению`, 
filename, err, output, compileInfo, originalCode, filename, langInfo.Extension)
			} else {
				fixPrompt = fmt.Sprintf(`Исправь следующий код (но не меняй его кардинально, а внеси точечные изменения), который вызвал ошибку:

Файл: %s
Ошибка: %v
Вывод компилятора: %s

ТЕКУЩИЙ КОД:
%s

ВЕРНИ ТОЛЬКО исправленный код в формате:
--- File: %s ---
<исправленный код здесь, без markdown>

ВАЖНО: 
- НЕ используйте markdown 
- Код должен быть чистым и готовым к выполнению`, 
filename, err, output, originalCode, filename)
			}
			
			// Отправляем в LLM
			fixedCode, llmErr := SendMessageToLLM(context.Background(), fixPrompt, provider, model, apiKey)
			if llmErr != nil {
				return fmt.Errorf("не удалось получить исправление от LLM: %v", llmErr)
			}

			// Парсим ответ
			parser := NewCodeParser()
			files := parser.ParseCodeBlocks(fixedCode)

			if len(files) == 0 {
				return fmt.Errorf("LLM не предоставил исправленный код")
			}

			fixedFile := files[0]
			fullPath := file //filepath.Join(".", file)
			if len(files) > 0 {
                if err := os.WriteFile(fullPath, []byte(fixedFile.Content), 0644); err != nil {
                    return fmt.Errorf("не удалось записать исправленный код: %v", err)
                }
                fmt.Println("✅ Исправленный код записан, повторяю компиляцию...")
            }
			
			// Обновляем compileInfo, если LLM предоставил новую
			if fixedFile.Compile != nil {
				compileInfo = fixedFile.Compile
			}
 
			fmt.Println("✅ Код исправлен, повторяю компиляцию...")
			time.Sleep(1 * time.Second) // Небольшая пауза
		}
	}

	return fmt.Errorf("не удалось запустить код после %d попыток", cr.maxRetries)
}

// executeInstallCommand выполняет команду установки зависимостей и выводит результаты в канвас
func (cr *CodeRunner) executeInstallCommand(ctx context.Context, command string) error {
	// Проверяем отмену контекста
	select {
	case <-ctx.Done():
		return fmt.Errorf("установка отменена пользователем")
	default:
	}

	// Разбиваем команду на части
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return fmt.Errorf("пустая команда установки")
	}

	// Первый аргумент - это команда (pip, apt-get, npm и т.д.)
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	// Запускаем команду
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("команда '%s' завершилась с ошибкой: %w", command, err)
	}

	return nil
}

// runCode выполняет компиляцию и запуск кода
func (cr *CodeRunner) runCode(file string, langInfo *LanguageInfo, compileInfo *CompileInfo) (string, error) {
	dir := filepath.Dir(file)
	filename := filepath.Base(file)

	// Меняем директорию на директорию файла
	originalDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(originalDir)
	// Сборка команды с учетом CompileInfo
	cmd, outputFile, err := cr.buildCompileCommand(filename, langInfo, compileInfo)
	if err != nil {
		return "", err
	}
	
	// Для HTML возвращаем специальный результат
	if langInfo.Extension == ".html" && cmd == nil {
        absPath, _ := filepath.Abs(filename)
		return fmt.Sprintf("HTML файл открыт в браузере: %s", "file://"+absPath), nil
	}

	// Выполняем команду
	var output []byte
	var runErr error
	
	if cmd != nil {
		output, err = cmd.CombinedOutput()
		if err != nil {
			return string(output), fmt.Errorf("ошибка выполнения: %v", err)
		}
	}

	// Если был создан outputFile, это компилируемый язык, нужно запустить
	if outputFile != "" && langInfo.Extension != ".kt" {
		// Выдаём права на выполнение, если нужно
		if langInfo.NeedsCompile && langInfo.Extension != ".html" {
			os.Chmod(outputFile, 0755)
		}
		
		// Запускаем скомпилированный файл
		runCmd := exec.Command("./" + outputFile)
		output, runErr = runCmd.CombinedOutput()
		if runErr != nil {
			return string(output), fmt.Errorf("ошибка запуска: %v", runErr)
		}
	}

	return string(output), nil
}

// RunDiffWithRetry запускает компиляцию/выполнение файла с автоисправлением ошибок в режиме DIFF.
// Теперь работает с частичными исправлениями.
func (cr *CodeRunner) RunDiffWithRetry(ctx context.Context, file string, provider, model, apiKey string, diffProcessor *DiffProcessor, compileInfo *CompileInfo) error {
	langInfo := cr.getLanguageInfo(file)
	if langInfo == nil {
		return fmt.Errorf("неподдерживаемый язык: %s", file)
	}

	// Пропускаем файлы, которые не нуждаются в компиляции
	if langInfo.Extension == ".html" || langInfo.Extension == ".txt" {
		return nil
	}

	fullPath := file
	
	// Счетчик успешных попыток для этого файла
	var successAttempts int
	var lastError error
	
	for attempt := 1; attempt <= cr.maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			fmt.Println("  🤖 Проверка отменена пользователем")
			return fmt.Errorf("проверка отменена")
		default:
		}
		
		fmt.Printf("  Попытка запуска %d/%d...\n", attempt, cr.maxRetries)

		output, err := cr.runCode(fullPath, langInfo, compileInfo)
		if err == nil {
			fmt.Printf("  ✅ Выполнение успешно!\n")
			if output != "" {
				fmt.Printf("  Вывод: %s\n", output)
			}
			return nil
		}

		// Запоминаем последнюю ошибку
		lastError = fmt.Errorf("%v", err)
		
		// Ошибка — показываем детали
		fmt.Printf("  ❌ Ошибка: %v\n", err)
		if output != "" {
			fmt.Printf("  Вывод компилятора:\n%s\n", output)
		}

		if attempt >= cr.maxRetries {
			// Превышен лимит попыток
			fmt.Printf("  ⚠️  Достигнут лимит попыток (%d)\n", cr.maxRetries)
			break
		}

		fmt.Println("  🤖 Пробую исправить код с помощью LLM в DIFF-формате...")

		// Читаем текущее состояние файла после предыдущих патчей
		content, readErr := os.ReadFile(fullPath)
		if readErr != nil {
			return fmt.Errorf("не удалось прочитать файл: %v", readErr)
		}

		// Формируем промпт для LLM с требованием вернуть только DIFF
		filename := filepath.Base(file)
		fixPrompt := fmt.Sprintf(`Исправь ОДНУ конкретную ошибку в коде (но не меняй код кардинально, а сделай точечные изменения), используя ТОЛЬКО DIFF-формат.
Не пытайся исправить все сразу. Сфокусируйся на самой первой/очевидной ошибке.

ФАЙЛ: %s
ОШИБКА: %v
ВЫВОД КОМПИЛЯТОРА:
%s

ТЕКУЩИЙ КОД:
%s

ФОРМАТ ОТВЕТА (только DIFF):
--- Diff: %s ---
Original lines X-Y:
строка1
строка2
Modified:
новая строка1
новая строка2

ВАЖНО:
1. Исправь только ОДНУ ошибку за раз
2. Сохраняй отступы
3. Укажи точные номера строк или контекст
4. Верни ТОЛЬКО DIFF-блок, без пояснений
5. НИКОГДА НЕ ВСТАВЛЯЙ МАРКЕРЫ '--- File:' ВНУТРЬ КОДА`, 
			filename, err, output, string(content), file)

		// Запрашиваем исправление у LLM
		fixedResponse, llmErr := SendMessageToLLM(context.Background(), fixPrompt, provider, model, apiKey)
		if llmErr != nil {
			fmt.Printf("  ❌ LLM ошибка: %v\n", llmErr)
			// Продолжаем следующую попытку
			continue
		}

		// Пробуем распарсить как DIFF
		fixBlocks := diffProcessor.ParseDiffBlocks(fixedResponse)
		if len(fixBlocks) == 0 {
			// Fallback: если LLM вернул целый файл
			parser := NewCodeParser()
			files := parser.ParseCodeBlocks(fixedResponse)
			if len(files) == 0 {
				fmt.Printf("  ❌ LLM не предоставил исправлений\n")
				continue
			}
			// Перезаписываем файл целиком
			if writeErr := os.WriteFile(fullPath, []byte(files[0].Content), 0644); writeErr != nil {
				fmt.Printf("  ❌ Ошибка записи: %v\n", writeErr)
				continue
			}
			fmt.Println("  ✅ Исправлено (замена файла), повторяю проверку...")
		} else {
			// Применяем DIFF-исправления
			if applyErr := diffProcessor.ApplyDiffBlocks(fixBlocks, true); applyErr != nil {
				fmt.Printf("  ❌ Ошибка применения патчей: %v\n", applyErr)
				// Даже при ошибках применения продолжаем попытки
				// Возможно некоторые патчи применились
			} else {
				fmt.Println("  ✅ Исправлено (DIFF-патчи), повторяю проверку...")
			}
		}

		// Увеличиваем счетчик успешных исправлений
		successAttempts++
		
		// Небольшая пауза между попытками
		time.Sleep(1 * time.Second)
	}

	// Возвращаем последнюю ошибку, если ни одна попытка не удалась
	if successAttempts == 0 {
		return fmt.Errorf("не удалось исправить код после %d попыток. Последняя ошибка: %v", 
			cr.maxRetries, lastError)
	}
	
	// Если были успешные исправления, но файл всё ещё содержит ошибки
	return fmt.Errorf("частично исправлено (%d/%d попыток), но остались ошибки: %v", 
		successAttempts, cr.maxRetries, lastError)
}

// RunProject запускает проект на основе конфигурации
func (cr *CodeRunner) RunProject(ctx context.Context, config *ProjectConfig, provider, model, apiKey string) error {
	if config.Language == "text" {
		fmt.Println("📄 Текстовый файл, пропущен запуск")
		return nil
	}

	if config.HasMakefile {
		return cr.runMakefileProject(ctx, config, provider, model, apiKey)
	}

	if config.Language == "go" && config.HasGoMod {
        return cr.runGoProject(ctx, config, provider, model, apiKey)
    }
    
    // ВСЕ Python-проекты проходят через единый обработчик
	if config.Language == "python" {
        // Добавить это условие для одиночных файлов:
        if config.HasPyMain == "" && len(config.Files) == 1 {
            fullPath := filepath.Join(".", config.EntryPoint)
            content, err := os.ReadFile(fullPath)
            if err != nil {
                return fmt.Errorf("не удалось прочитать %s: %v", config.EntryPoint, err)
            }
            return cr.RunWithRetry(ctx, fullPath, string(content), provider, model, apiKey, nil)
        }
        return cr.runPythonProject(ctx, config, provider, model, apiKey)
    }	

	// Для компилируемых языков с множественными файлами
	if config.CompileCommand != "" {
		return cr.runCompiledProject(ctx, config, provider, model, apiKey)
	}

	// Дефолт: запуск одного файла
	if config.EntryPoint == "" {
		return fmt.Errorf("не найдена точка входа")
	}

	fullPath := filepath.Join(".", config.EntryPoint)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Errorf("не удалось прочитать %s: %v", config.EntryPoint, err)
	}

	// Добавляем аргументы к любой команде запуска
    if config.RunCommand != "" && len(config.Args) > 0 {
        config.RunCommand += " " + strings.Join(config.Args, " ")
    }	
	return cr.RunWithRetry(ctx, fullPath, string(content), provider, model, apiKey, nil)
}

func (cr *CodeRunner) runCompiledProject(ctx context.Context, config *ProjectConfig, provider, model, apiKey string) error {
    if config.CompileCommand == "" {
        return fmt.Errorf("не найдена команда компиляции")
    }

    // Добавить циклы попыток:
    for attempt := 1; attempt <= cr.maxRetries; attempt++ {
        fmt.Printf("  Попытка компиляции %d/%d...\n", attempt, cr.maxRetries)
        
        cmd := exec.Command("sh", "-c", config.CompileCommand)
        cmd.Stdout = os.Stdout
        cmd.Stderr = os.Stderr
        
        if err := cmd.Run(); err != nil {
            fmt.Printf("❌ Ошибка компиляции: %v\n", err)
            
            if attempt < cr.maxRetries {
                fmt.Println("🤖 Пробую исправить ошибку...")
                
                // Исправить главный файл
                entry := config.EntryPoint
                if entry == "" && len(config.Files) > 0 {
                    entry = config.Files[0]
                }
                if entry == "" {
                    return err
                }
                
                src, _ := os.ReadFile(entry)
                cr.RunWithRetry(ctx, entry, string(src), provider, model, apiKey, nil)
            } else {
                return err
            }
        } else {
            break // Успешная компиляция
        }
    }

    // Запуск (как было)
    runCmd := config.RunCommand
    if len(config.Args) > 0 {
        runCmd += " " + strings.Join(config.Args, " ")
    }
    fmt.Printf("  Запуск: %s\n", runCmd)
    cmd := exec.Command("sh", "-c", runCmd)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    return cmd.Run()
}

// runGoProject запускает Go модуль
func (cr *CodeRunner) runGoProject(ctx context.Context, config *ProjectConfig, provider, model, apiKey string) error {
	fmt.Println("  Запуск как Go модуль: go run .")
	cmd := exec.Command("go", "run", ".")
	if len(config.Args) > 0 {
		cmd.Args = append(cmd.Args, config.Args...)
		// if len(config.Args) > 0 && cr.config.GetBool("debug_mode") {
            // fmt.Printf("  Аргументы: %v\n", config.Args)
        // }
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	if err := cmd.Run(); err != nil {
		return cr.handleRuntimeError(ctx, err, config, provider, model, apiKey)
	}
	return nil
}

// runPythonPackage запускает Python пакет
func (cr *CodeRunner) runPythonProject(ctx context.Context, config *ProjectConfig, provider, model, apiKey string) error {
    var cmd *exec.Cmd
    
    if config.HasPyMain != "" {
        dir := filepath.Dir(config.HasPyMain)
        fmt.Printf("  Запуск как Python пакет: python3 -m %s\n", filepath.Base(dir))
        cmd = exec.Command("python3", "-m", filepath.Base(dir))
    } else if config.EntryPoint != "" {
        fmt.Printf("  Запуск Python скрипта: python3 %s\n", config.EntryPoint)
        cmd = exec.Command("python3", config.EntryPoint)
    } else {
        return fmt.Errorf("не найдена точка входа Python")
    }

    if len(config.Args) > 0 {
        cmd.Args = append(cmd.Args, config.Args...)
        // if len(config.Args) > 0 && cr.config.GetBool("debug_mode") {
            // fmt.Printf("  Аргументы: %v\n", config.Args)
        // }
    }
    
    // Добавляем перенаправление вывода
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr

    // Обрабатываем ошибки
    if err := cmd.Run(); err != nil {
        return cr.handleRuntimeError(ctx, err, config, provider, model, apiKey)
    }
    return nil
}

// runMakefileProject запускает через make
func (cr *CodeRunner) runMakefileProject(ctx context.Context, config *ProjectConfig, provider, model, apiKey string) error {
	fmt.Println("  Использование Makefile: make")
	cmd := exec.Command("make")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ошибка make: %v", err)
	}
	
	if config.RunCommand != "" {
		fmt.Printf("  Запуск: %s\n", config.RunCommand)
		cmd = exec.Command("sh", "-c", config.RunCommand)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	return nil
}

// handleRuntimeError обрабатывает ошибки выполнения проекта (упрощённо)
func (cr *CodeRunner) handleRuntimeError(ctx context.Context, runErr error, config *ProjectConfig, provider, model, apiKey string) error {
	fmt.Println("🤖 Пробую исправить ошибку выполнения...")
	fmt.Printf("⚠️  Ошибка: %v\n", runErr)
	// Здесь можно добавить логику автоисправления через LLM, если нужно
	return runErr
}
