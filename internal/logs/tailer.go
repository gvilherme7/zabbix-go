package logs

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

// TailLog reads a file and finds the last match of the regex.
// In a full implementation, this tracks inodes/offsets.
func TailLog(filePath string, regexPattern string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var matches []string
	re, err := regexp.Compile(regexPattern)
	if err != nil {
		return "", err
	}

	for scanner.Scan() {
		line := scanner.Text()
		if re.MatchString(line) {
			matches = append(matches, line)
		}
	}
	
	if len(matches) > 0 {
		// Return last matched line
		return strings.TrimSpace(matches[len(matches)-1]), nil
	}
	return "No matches", nil
}
