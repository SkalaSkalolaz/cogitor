// codeparser.go
// Парсинг ответов LLM и извлечение блоков кода в формате "--- File: path ---"

package main

import (
	"regexp"
	"strings"
)

// CompileInfo содержит информацию о компиляции для файла
type CompileInfo struct {
	Language string
	Flags    string
	Command  string
	InstallCommand string
}

// CodeParser парсит код из ответов LLM
type CodeParser struct{}

// NewCodeParser создает новый парсер кода
func NewCodeParser() *CodeParser {
	return &CodeParser{}
}

// CodeFile представляет файл с кодом и опциональной информацией о компиляции
type CodeFile struct {
	Path    string
	Content string
	Compile *CompileInfo // может быть nil, если нет спец. флагов
}

// ParseCodeBlocks парсит ответ LLM и извлекает файлы с кодом и информацию о компиляции
func (cp *CodeParser) ParseCodeBlocks(response string) []CodeFile {
	var files []CodeFile

	// Регулярное выражение для поиска блоков "--- File: path ---"
	// Захватывает также возможный следующий блок "--- Compile: ---"
	filePattern := regexp.MustCompile(`---\s*[Ff]ile:\s*([^\n]+)\s*---\s*\n([\s\S]*?)(?:\n---\s*[Cc]ompile:\s*|\n---\s*[Ff]ile:|\z)`)
	matches := filePattern.FindAllStringSubmatch(response, -1)

	for i, match := range matches {
		if len(match) >= 3 {
			path := strings.TrimSpace(match[1])
			content := strings.TrimSpace(match[2])
			// Очистка от вложенных маркеров
            content = cp.cleanCodeFromMarkers(content)

			// Пропускаем пустые файлы
			if path != "" && content != "" {
				codeFile := CodeFile{
					Path:    path,
					Content: content,
				}

				// Проверяем, есть ли после этого файла информация о компиляции
				if i < len(matches)-1 {
					// Смотрим между текущим и следующим File блоком
					nextMatchStart := strings.Index(response, matches[i+1][0])
					currentEnd := strings.Index(response, match[0]) + len(match[0])
					between := response[currentEnd:nextMatchStart]
					
					if compileInfo := cp.parseCompileInfo(between); compileInfo != nil {
						codeFile.Compile = compileInfo
					}
				} else {
					// Для последнего файла - смотрим до конца строки
					currentEnd := strings.Index(response, match[0]) + len(match[0])
					between := response[currentEnd:]
					
					if compileInfo := cp.parseCompileInfo(between); compileInfo != nil {
						codeFile.Compile = compileInfo
					}
				}

				files = append(files, codeFile)
			}
		}
	}

	return files
}

func (cp *CodeParser) cleanCodeFromMarkers(content string) string {
    lines := strings.Split(content, "\n")
    var cleaned []string
    
    for _, line := range lines {
        trimmed := strings.TrimSpace(line)
        if strings.HasPrefix(trimmed, "--- File:") || 
           strings.HasPrefix(trimmed, "--- Diff:") ||
           strings.HasPrefix(trimmed, "--- Compile:") ||
           strings.HasPrefix(trimmed, "--- Install:") {
           continue // Пропускаем строки с маркерами
        }
        cleaned = append(cleaned, line)
    }
    
    return strings.Join(cleaned, "\n")
}

func (cp *CodeParser) parseCompileInfo(text string) *CompileInfo {
	// Универсальное регулярное выражение для обоих типов блоков
	pattern := regexp.MustCompile(`---\s*[Cc]ompile:\s*([^\n]+)\s*---\s*\n?([\s\S]*?)(?:\n---\s*[Ee]nd\s*[Cc]ompile|\n---\s*[Cc]ompile:|\n---\s*[Ii]nstall:|\n---\s*[Ff]ile:|\z)`)
	matches := pattern.FindStringSubmatch(text)
	
	if len(matches) >= 3 {
		languageLine := strings.TrimSpace(matches[1])
		flags := strings.TrimSpace(matches[2])
		
		parts := strings.SplitN(languageLine, ":", 2)
		language := strings.TrimSpace(parts[0])
		
		compileInfo := &CompileInfo{
			Language: language,
		}
		
		if len(parts) > 1 {
			cmdPart := strings.TrimSpace(parts[1])
			if strings.Contains(cmdPart, " ") && (strings.HasPrefix(cmdPart, "gcc") || 
				strings.HasPrefix(cmdPart, "g++") || strings.HasPrefix(cmdPart, "go ") ||
				strings.HasPrefix(cmdPart, "python") || strings.Contains(cmdPart, "pip install")) {
				compileInfo.Command = cmdPart
			} else {
				compileInfo.Flags = cmdPart
			}
		} else if flags != "" {
			if strings.Contains(flags, " ") && (strings.HasPrefix(flags, "gcc") || 
				strings.HasPrefix(flags, "g++") || strings.HasPrefix(flags, "go ") ||
				strings.Contains(flags, "pip install")) {
				compileInfo.Command = flags
			} else {
				compileInfo.Flags = flags
			}
		}
		
		return compileInfo
	}
	
	// Парсим Install блок
	installPattern := regexp.MustCompile(`---\s*[Ii]nstall:\s*([^\n]+)\s*---\s*\n?([\s\S]*?)(?:\n---\s*[Ee]nd\s*[Ii]nstall|\n---\s*[Cc]ompile:|\n---\s*[Ff]ile:|\z)`)
	installMatches := installPattern.FindStringSubmatch(text)
	
	if len(installMatches) >= 3 {
		// Мы не используем язык из Install блока, только команду
		command := strings.TrimSpace(installMatches[2])
		return &CompileInfo{
			InstallCommand: command,
		}
	}
	
	return nil
}

// IsCodeResponse быстро проверяет, содержит ли ответ блоки кода
func (cp *CodeParser) IsCodeResponse(response string) bool {
	return strings.Contains(response, "--- File:") || 
		   strings.Contains(response, "--- Diff:")
}