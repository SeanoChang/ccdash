package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const teeMarker = "# llm-usage-dashboard capture"

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func teeSnippet(capturePath string) string {
	return fmt.Sprintf("\n%s\nprintf '%%s\\n' \"$input\" >> %s\n", teeMarker, shellQuote(capturePath))
}

func alreadyInstalled(script, capturePath string) bool {
	return strings.Contains(script, teeMarker) || strings.Contains(script, capturePath)
}

func statuslineDiff(scriptPath, script, snippet string) string {
	line := strings.Count(script, "\n") + 1
	added := strings.Split(strings.Trim(snippet, "\n"), "\n")
	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n+++ %s\n@@ -%d,0 +%d,%d @@\n",
		scriptPath, scriptPath, line, line, len(added))
	for _, value := range added {
		fmt.Fprintf(&out, "+%s\n", value)
	}
	return out.String()
}

// setupStatusline is the sole operation allowed to write beneath ~/.claude.
// It displays the append exactly, confirms it, and preserves a backup first.
func setupStatusline() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	scriptPath := filepath.Join(home, ".claude", "statusline-command.sh")
	capturePath := filepath.Join(dataDir(), "statusline.jsonl")
	script, err := os.ReadFile(scriptPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no statusline script at %s — configure one in Claude Code first", scriptPath)
		}
		return err
	}
	if alreadyInstalled(string(script), capturePath) {
		fmt.Println("capture already installed; nothing to do")
		return nil
	}
	info, err := os.Stat(scriptPath)
	if err != nil {
		return err
	}
	snippet := teeSnippet(capturePath)
	fmt.Print(statuslineDiff(scriptPath, string(script), snippet))
	fmt.Printf("A backup will be written alongside the script.\nProceed? [y/N] ")
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		fmt.Printf("aborted; to install manually, append:\n%s", snippet)
		return nil
	}

	current, err := os.ReadFile(scriptPath)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, script) {
		return errors.New("statusline script changed while awaiting confirmation; rerun setup-statusline")
	}
	if err := os.MkdirAll(filepath.Dir(capturePath), 0o755); err != nil {
		return err
	}
	backupPath := fmt.Sprintf("%s.bak.%d", scriptPath, time.Now().UnixNano())
	backup, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := backup.Write(script); err != nil {
		backup.Close()
		return err
	}
	if err := backup.Close(); err != nil {
		return err
	}
	file, err := os.OpenFile(scriptPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(snippet); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	fmt.Printf("installed; backup at %s\ncapturing to %s\n", backupPath, capturePath)
	return nil
}
