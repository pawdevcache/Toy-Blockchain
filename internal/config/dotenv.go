package config

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// loadDotEnv reads a minimal KEY=VALUE file and exports any key that is not
// already set in the process environment, so a real environment variable always
// beats the file. A missing file is ignored on purpose.
//
// Written by hand rather than pulling in github.com/joho/godotenv: the format we
// need is 20 lines of stdlib, and the assessment asks us to justify every
// third-party dependency.
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue // blank line or comment
		}
		key, value, ok := strings.Cut(text, "=")
		if !ok {
			return fmt.Errorf("%s:%d: expected KEY=VALUE, got %q", path, line, text)
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if _, set := os.LookupEnv(key); !set {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("%s:%d: %w", path, line, err)
			}
		}
	}
	return scanner.Err()
}
