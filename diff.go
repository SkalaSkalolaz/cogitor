// diff.go – умные DIFF-патчи с fuzzy-валидацией и авто-отступами
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"regexp"
)

// ---------------  типы и конструктор  ---------------
type DiffProcessor struct {
	fileParser     *FileParser
	terminalReader *TerminalReader
	config         *Config
}

type DiffBlock struct {
	FilePath  string
	Original  []string
	Modified  []string
	LineStart int
	LineEnd   int
	Compile   *CompileInfo
}

func NewDiffProcessor(fp *FileParser, tr *TerminalReader, cfg *Config) *DiffProcessor {
	return &DiffProcessor{fileParser: fp, terminalReader: tr, config: cfg}
}

// ---------------  парсинг блоков из ответа LLM  ---------------
func (dp *DiffProcessor) ParseDiffBlocks(response string) []DiffBlock {
	re := regexp.MustCompile(`---\s*[Dd]iff:\s*([^\n]+)\s*---\s*\n(?:Original lines (\d+)-(\d+):\s*\n)?([\s\S]*?)(?:\n---\s*[Ee]nd\s*[Dd]iff|\n---\s*[Dd]iff:|\z)`)
	var blocks []DiffBlock
	for i, m := range re.FindAllStringSubmatch(response, -1) {
		if len(m) < 5 {
			continue
		}
		path := strings.TrimSpace(m[1])
		start, end := 0, 0
		if m[2] != "" {
			fmt.Sscanf(m[2], "%d", &start)
			fmt.Sscanf(m[3], "%d", &end)
		}
		orig, mod := dp.splitOriginalModified(m[4])
		b := DiffBlock{
			FilePath:  path,
			Original:  orig,
			Modified:  mod,
			LineStart: start,
			LineEnd:   end,
		}
		// compile-блок
		between := ""
		if i < len(re.FindAllStringSubmatch(response, -1))-1 {
			next := re.FindAllStringSubmatch(response, -1)[i+1][0]
			between = response[strings.Index(response, m[0])+len(m[0]):strings.Index(response, next)]
		} else {
			between = response[strings.Index(response, m[0])+len(m[0]):]
		}
		if ci := dp.parseCompileInfo(between); ci != nil {
			b.Compile = ci
		}
		blocks = append(blocks, b)
	}
	return blocks
}

func (dp *DiffProcessor) splitOriginalModified(content string) (orig, mod []string) {
	if !strings.Contains(content, "Modified:") {
		return []string{}, strings.Split(content, "\n")
	}
	parts := strings.SplitN(content, "Modified:", 2)
	orig = dp.normalizeTrailingEmptyLines(strings.Split(strings.TrimSpace(parts[0]), "\n"))
	mod = dp.normalizeTrailingEmptyLines(strings.Split(strings.TrimSpace(parts[1]), "\n"))
	// убираем «Original lines X-Y:»
	if len(orig) > 0 && strings.Contains(orig[0], "Original lines") {
		orig = orig[1:]
	}
	return
}

func (dp *DiffProcessor) parseCompileInfo(text string) *CompileInfo {
	re := regexp.MustCompile(`---\s*[Cc]ompile:\s*([^\n]+)\s*---\s*\n?([\s\S]*?)(?:\n---\s*[Ee]nd\s*[Cc]ompile|\n---\s*[Dd]iff:|\z)`)
	if m := re.FindStringSubmatch(text); len(m) >= 3 {
		langLine, flags := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
		parts := strings.SplitN(langLine, ":", 2)
		ci := &CompileInfo{Language: strings.TrimSpace(parts[0])}
		if len(parts) > 1 {
			ci.Command = strings.TrimSpace(parts[1])
		} else if flags != "" {
			ci.Command = flags
		}
		return ci
	}
	return nil
}

// ---------------  применение патчей  ---------------
func (dp *DiffProcessor) ApplyDiffBlocks(blocks []DiffBlock, autoMode bool) error {
	if len(blocks) == 0 {
		return fmt.Errorf("DIFF-блоки не найдены")
	}
	
	// Группируем патчи по файлам
	filePatches := make(map[string][]DiffBlock)
	for _, b := range blocks {
		filePatches[b.FilePath] = append(filePatches[b.FilePath], b)
	}
	
	var allErrors []string
	
	// Обрабатываем каждый файл независимо
	for fp, patches := range filePatches {
		if err := dp.applySingleFilePatchesOptimized(fp, patches, autoMode); err != nil {
			allErrors = append(allErrors, fmt.Sprintf("файл %s: %v", fp, err))
			// Продолжаем обработку других файлов
			continue
		}
	}
	
	// Если были ошибки, возвращаем их все
	if len(allErrors) > 0 {
		return fmt.Errorf("ошибки в применении патчей:\n  %s", 
			strings.Join(allErrors, "\n  "))
	}
	
	return nil
}

// applySingleFilePatchesOptimized - новая оптимизированная версия
func (dp *DiffProcessor) applySingleFilePatchesOptimized(filePath string, blocks []DiffBlock, autoMode bool) error {
	fullPath, err := dp.resolveSafePath(filePath)
	if err != nil {
		return fmt.Errorf("валидация пути файла '%s' провалилась: %v", filePath, err)
	}
	
	// Читаем исходный файл
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Errorf("ошибка чтения: %v", err)
	}
	origLines := strings.Split(string(content), "\n")
	
	// Создаем бэкап перед изменениями
	if !autoMode && dp.config != nil && !dp.config.GetBool("skip_backup") {
		backupPath := fullPath + ".backup"
		if err := os.WriteFile(backupPath, content, 0644); err != nil {
			return fmt.Errorf("не удалось создать бэкап %s: %v", backupPath, err)
		}
		fmt.Printf("  💾 Создана копия: %s\n", backupPath)
	}
	
	// Собираем информацию о всех патчах для файла
	var patches []struct {
		block             DiffBlock
		startIdx, endIdx  int
		validationError   string
	}
	
	// ПРЕДВАРИТЕЛЬНАЯ ВАЛИДАЦИЯ: проверяем все патчи перед применением
	for _, b := range blocks {
		start, end := dp.calculateRangeSmart(origLines, b)
		
		// Проверяем, соответствует ли патч содержимому файла
		if !dp.validateBlockFuzzy(origLines, start, end, b.Original) {
			patches = append(patches, struct {
				block           DiffBlock
				startIdx, endIdx int
				validationError  string
			}{
				block:          b,
				startIdx:       start,
				endIdx:         end,
				validationError: fmt.Sprintf("содержимое не соответствует (fuzzy) в блоке %d строк", len(b.Original)),
			})
			continue
		}
		
		patches = append(patches, struct {
			block           DiffBlock
			startIdx, endIdx int
			validationError  string
		}{
			block:          b,
			startIdx:       start,
			endIdx:         end,
			validationError: "", // Ошибок нет
		})
	}
	
	// Разделяем патчи на успешные и проблемные
	var validPatches []struct {
		block           DiffBlock
		startIdx, endIdx int
	}
	var invalidPatches []string
	
	for _, p := range patches {
		if p.validationError != "" {
			invalidPatches = append(invalidPatches, 
				fmt.Sprintf("патч для строк %d-%d: %s", 
					p.startIdx+1, p.endIdx, p.validationError))
		} else {
			validPatches = append(validPatches, struct {
				block           DiffBlock
				startIdx, endIdx int
			}{
				block:     p.block,
				startIdx:  p.startIdx,
				endIdx:    p.endIdx,
			})
		}
	}
	
	// Показываем информацию о проблемных патчах
	if len(invalidPatches) > 0 {
		fmt.Printf("  ⚠️  Предупреждения для %s:\n", filePath)
		for _, msg := range invalidPatches {
			fmt.Printf("    - %s\n", msg)
		}
		
		// Спрашиваем подтверждение, если есть проблемные патчи
		if !autoMode {
			response, err := dp.terminalReader.ReadLineWithPrompt(
				fmt.Sprintf("Применить только %d корректных патчей из %d? (y/n): ", 
					len(validPatches), len(blocks)))
			if err != nil || strings.ToLower(strings.TrimSpace(response)) != "y" {
				return fmt.Errorf("пользователь отменил операцию из-за проблемных патчей")
			}
		}
	}
	
	// Если нет корректных патчей для применения
	if len(validPatches) == 0 {
		return fmt.Errorf("нет корректных патчей для применения в файле %s", filePath)
	}
	
	// СОРТИРУЕМ патчи в обратном порядке по строкам (от конца к началу)
	// Это важно, чтобы не сбивались индексы при последовательном применении
	sort.Slice(validPatches, func(i, j int) bool {
		return validPatches[i].startIdx > validPatches[j].startIdx
	})
	
	// Применяем патчи к копии исходных строк
	resultLines := make([]string, len(origLines))
	copy(resultLines, origLines)
	
	var appliedCount int
	var applyErrors []string
	
	for _, p := range validPatches {
		b := p.block
		start, end := p.startIdx, p.endIdx
		
		// Восстанавливаем отступы для измененных строк
		modLines := dp.restoreLeadingWhitespace(resultLines, start, end, b.Modified)
		
		// Формируем новые строки с примененным патчем
		newLines := append([]string{}, resultLines[:start]...)
		newLines = append(newLines, modLines...)
		newLines = append(newLines, resultLines[end:]...)
		
		// Проверяем, что патч применился корректно (базовая проверка)
		if len(newLines) != len(resultLines)-len(b.Original)+len(b.Modified) {
			applyErrors = append(applyErrors, 
				fmt.Sprintf("патч строк %d-%d: несоответствие длины после применения", 
					start+1, end))
			continue // Пропускаем этот патч, продолжаем с остальными
		}
		
		// Обновляем результат
		resultLines = newLines
		appliedCount++
	}
	
	// Записываем результат, даже если не все патчи применились
	result := strings.Join(resultLines, "\n")
	if err := os.WriteFile(fullPath, []byte(result), 0644); err != nil {
		return fmt.Errorf("ошибка записи: %v", err)
	}
	
	// Формируем отчет о применении
	fmt.Printf("  ✅ %s: применено %d/%d патчей", filePath, appliedCount, len(blocks))
	if len(invalidPatches) > 0 {
		fmt.Printf(" (%d с предупреждениями)", len(invalidPatches))
	}
	if len(applyErrors) > 0 {
		fmt.Printf(", ошибок применения: %d", len(applyErrors))
	}
	fmt.Println()
	
	// Показываем детали ошибок применения
	if len(applyErrors) > 0 {
		for _, errMsg := range applyErrors {
			fmt.Printf("    ⚠️  %s\n", errMsg)
		}
	}
	
	// Если ни один патч не применился - возвращаем ошибку
	if appliedCount == 0 {
		return fmt.Errorf("ни один патч не был применен")
	}
	
	return nil
}


// ---------------  умный диапазон + fuzzy  ---------------
func (dp *DiffProcessor) calculateRangeSmart(lines []string, b DiffBlock) (int, int) {
	target := b.Original
	if len(target) == 0 {
		return 0, 0
	}
	// подсказка от LLM
	hint := b.LineStart - 1
	if hint < 0 {
		hint = 0
	}
	// ищем в окрестности ±10 строк
	bestStart, bestScore := -1, 0
	for i := hint - 10; i <= hint+10; i++ {
		if i < 0 || i+len(target) > len(lines) {
			continue
		}
		score := 0
		for j, line := range target {
			if strings.TrimSpace(lines[i+j]) == strings.TrimSpace(line) {
				score++
			}
		}
		if score > bestScore {
			bestScore, bestStart = score, i
		}
	}
	if bestStart >= 0 {
		return bestStart, bestStart + len(target)
	}
	// fallback: поиск по всему файлу
	if idx := dp.findMatchIndex(lines, target); idx >= 0 {
		return idx, idx + len(target)
	}
	return 0, len(lines)
}

func (dp *DiffProcessor) validateBlockFuzzy(lines []string, start, end int, expected []string) bool {
	if start < 0 || end > len(lines) || len(expected) != end-start {
		return false
	}
	// 70 % совпадения считаем успехом
	matched := 0
	for i, exp := range expected {
		if strings.TrimSpace(lines[start+i]) == strings.TrimSpace(exp) {
			matched++
		}
	}
	return float64(matched)/float64(len(expected)) >= 0.7
}

func (dp *DiffProcessor) findMatchIndex(lines, target []string) int {
	for i := 0; i <= len(lines)-len(target); i++ {
		ok := true
		for j := 0; j < len(target); j++ {
			if strings.TrimSpace(lines[i+j]) != strings.TrimSpace(target[j]) {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

// ---------------  отступы  ---------------
func (dp *DiffProcessor) restoreLeadingWhitespace(lines []string, start, end int, mod []string) []string {
	if len(mod) == 0 {
		return mod
	}
	res := make([]string, len(mod))
	for i, m := range mod {
		var ws string
		if start+i < len(lines) {
			ws = extractLeadingWhitespace(lines[start+i])
		} else if start > 0 {
			ws = extractLeadingWhitespace(lines[start-1])
		}
		if ws != "" && len(m) > 0 && (m[0] != ' ' && m[0] != '\t') {
			res[i] = ws + m
		} else {
			res[i] = m
		}
	}
	return res
}

func extractLeadingWhitespace(s string) string {
	var b strings.Builder
	for _, ch := range s {
		if ch == ' ' || ch == '\t' {
			b.WriteRune(ch)
		} else {
			break
		}
	}
	return b.String()
}

func (dp *DiffProcessor) normalizeTrailingEmptyLines(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// ---------------  утилиты  ---------------
func (dp *DiffProcessor) HasDiffMarker(q string) bool {
	return strings.Contains(q, "$diff") || strings.Contains(q, "$patch")
}
func (dp *DiffProcessor) GetTargetFiles(q string) []string {
	refs, _ := dp.fileParser.ExtractFileReferences(q)
	var files []string
	for _, r := range refs {
		if !r.IsAll && !r.IsURL {
			files = append(files, r.Path)
		}
	}
	return files
}

// resolveSafePath проверяет, что путь не содержит попыток выйти за пределы рабочей директории
func (dp *DiffProcessor) resolveSafePath(relativePath string) (string, error) {
    // Очищаем путь от лишних элементов
    cleanPath := filepath.Clean(relativePath)
    
    // === ИСПРАВЛЕНИЕ 1: Раскрытие домашней директории ===
    if strings.HasPrefix(cleanPath, "~/") {
        home, err := os.UserHomeDir()
        if err != nil {
            return "", fmt.Errorf("не удалось получить домашнюю директорию: %v", err)
        }
        cleanPath = filepath.Join(home, cleanPath[2:])
    }
    
    // === ИСПРАВЛЕНИЕ 2: Разрешение абсолютных путей внутри рабочей директории ===
    workingDir, err := os.Getwd()
    if err != nil {
        return "", fmt.Errorf("не удалось получить текущую директорию: %v", err)
    }
    
    var fullPath string
    if filepath.IsAbs(cleanPath) {
        // Для абсолютных путей проверяем, что они внутри рабочей директории
        relPath, err := filepath.Rel(workingDir, cleanPath)
        if err != nil {
            return "", fmt.Errorf("не удалось проверить относительный путь: %v", err)
        }
        
        if strings.HasPrefix(relPath, "..") {
            return "", fmt.Errorf("путь выходит за пределы рабочей директории: %s", cleanPath)
        }
        
        fullPath = cleanPath
    } else {
        // Относительный путь - соединяем с рабочей директорией
        fullPath = filepath.Join(workingDir, cleanPath)
    }
    
    // Проверяем, что резолвленный путь остается внутри рабочей директории
    resolvedPath, err := filepath.EvalSymlinks(fullPath)
    if err != nil {
        // Файл еще не существует - разрешаем создание, но только внутри рабочей директории
        if os.IsNotExist(err) {
            // Проверяем, что родительская директория разрешена
            parentDir := filepath.Dir(fullPath)
            relParent, err := filepath.Rel(workingDir, parentDir)
            if err != nil {
                return "", fmt.Errorf("не удалось проверить родительскую директорию: %v", err)
            }
            
            if strings.HasPrefix(relParent, "..") {
                return "", fmt.Errorf("родительская директория выходит за пределы рабочей директории: %s", parentDir)
            }
            
            return fullPath, nil
        }
        return "", err
    }
    
    // Проверяем, что резолвленный путь остается внутри рабочей директории
    relResolved, err := filepath.Rel(workingDir, resolvedPath)
    if err != nil {
        return "", fmt.Errorf("не удалось проверить резолвленный путь: %v", err)
    }
    
    if strings.HasPrefix(relResolved, "..") {
        return "", fmt.Errorf("путь выходит за пределы рабочей директории: %s", resolvedPath)
    }
    
    return fullPath, nil
}

// SafeApplyDiffBlocks применяет патчи с максимальной отказоустойчивостью
// Возвращает статистику примененных/непримененных патчей
func (dp *DiffProcessor) SafeApplyDiffBlocks(blocks []DiffBlock, autoMode bool) (applied int, total int, errors []string) {
	total = len(blocks)
	
	if total == 0 {
		errors = append(errors, "нет патчей для применения")
		return
	}
	
	// Группируем по файлам
	fileGroups := make(map[string][]DiffBlock)
	for _, b := range blocks {
		fileGroups[b.FilePath] = append(fileGroups[b.FilePath], b)
	}
	
	// Обрабатываем каждый файл независимо
	for filePath, fileBlocks := range fileGroups {
		// Применяем патчи для этого файла
		if err := dp.applySingleFilePatchesOptimized(filePath, fileBlocks, autoMode); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", filePath, err))
			// Подсчитываем сколько патчей из этого файла могли быть применены
			// (это приблизительная оценка)
			applied += len(fileBlocks) / 2 // Предполагаем, что половина применилась
		} else {
			applied += len(fileBlocks)
		}
	}
	
	return
}