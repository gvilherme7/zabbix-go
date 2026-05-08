package main

import (
	"bufio"
	"os"
	"strings"
)

type ConfigParams struct {
	TLSConnect  string
	TLSCAFile   string
	TLSCertFile string
	TLSKeyFile  string
}

// ParseConfig reads a Zabbix-style configuration file and extracts
// relevant parameters, focusing on UserParameters.
func ParseConfig(filePath string) (ConfigParams, error) {
	var params ConfigParams
	if filePath == "" {
		return params, nil
	}

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return params, err
		}
		return params, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Ignore empty lines and comments
		if len(line) == 0 || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "TLSConnect=") {
			params.TLSConnect = strings.TrimSpace(line[len("TLSConnect="):])
		} else if strings.HasPrefix(line, "TLSCAFile=") {
			params.TLSCAFile = strings.TrimSpace(line[len("TLSCAFile="):])
		} else if strings.HasPrefix(line, "TLSCertFile=") {
			params.TLSCertFile = strings.TrimSpace(line[len("TLSCertFile="):])
		} else if strings.HasPrefix(line, "TLSKeyFile=") {
			params.TLSKeyFile = strings.TrimSpace(line[len("TLSKeyFile="):])
		} else if strings.HasPrefix(line, "UserParameter=") {
			val := line[len("UserParameter="):]
			parts := strings.SplitN(val, ",", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				command := strings.TrimSpace(parts[1])
				Registry[key] = &UserParamPlugin{key: key, command: command}
			}
		}
	}

	return params, scanner.Err()
}
