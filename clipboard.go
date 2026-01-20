// clipboard.go
package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// ReadClipboard читает содержимое буфера обмена
func ReadClipboard() (string, error) {
	var cmd *exec.Cmd
	
	switch runtime.GOOS {
	case "darwin": // macOS
		cmd = exec.Command("pbpaste")
	case "windows":
		cmd = exec.Command("powershell", "-command", "Get-Clipboard")
	default: // Linux и другие UNIX-системы
		// Пробуем разные команды для Linux
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard", "-out")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command("xsel", "--clipboard", "--output")
		} else {
			return "", fmt.Errorf("не найдены команды для работы с буфером обмена (xclip или xsel)")
		}
	}
	
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ошибка чтения буфера обмена: %w", err)
	}
	
	return strings.TrimSpace(string(output)), nil
}

// WriteClipboard записывает текст в буфер обмена
func WriteClipboard(text string) error {
	var cmd *exec.Cmd
	
	switch runtime.GOOS {
	case "darwin": // macOS
		cmd = exec.Command("pbcopy")
	case "windows":
		cmd = exec.Command("powershell", "-command", "Set-Clipboard")
	default: // Linux и другие UNIX-системы
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard", "-in")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		} else {
			return fmt.Errorf("не найдены команды для работы с буфером обмена (xclip или xsel)")
		}
	}
	
	cmd.Stdin = strings.NewReader(text)
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("ошибка записи в буфер обмена: %w", err)
	}
	
	return nil
}

// CheckClipboardSupport проверяет поддержку буфера обмена в системе
func CheckClipboardSupport() bool {
	switch runtime.GOOS {
	case "darwin":
		_, err := exec.LookPath("pbpaste")
		return err == nil
	case "windows":
		return true // PowerShell всегда доступен в Windows
	default:
		_, err1 := exec.LookPath("xclip")
		_, err2 := exec.LookPath("xsel")
		return err1 == nil || err2 == nil
	}
}

// GetClipboardStatus возвращает статус поддержки буфера обмена
func GetClipboardStatus() string {
	if CheckClipboardSupport() {
		switch runtime.GOOS {
		case "darwin":
			return "✅ Буфер обмена доступен (macOS pbcopy/pbpaste)"
		case "windows":
			return "✅ Буфер обмена доступен (Windows PowerShell)"
		default:
			if _, err := exec.LookPath("xclip"); err == nil {
				return "✅ Буфер обмена доступен (Linux xclip)"
			} else if _, err := exec.LookPath("xsel"); err == nil {
				return "✅ Буфер обмена доступен (Linux xsel)"
			}
		}
	}
	return "❌ Буфер обмена не доступен - установите xclip или xsel для Linux"
}