// Create new file: cf/logger.go
package cf

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	maxLogSize    = 10 * 1024 * 1024 // 10MB
	maxLogFiles   = 3                // Keep 3 backup files
	cfLogFileName = "cfDebug.log"
)

var (
	cfLogger   *log.Logger
	cfLogFile  *os.File
	cfLogMutex sync.Mutex
	cfLogSize  int64
	cfLogDir   string
)

// InitCFLogger initializes the CloudFlare debug logger
// This should be called once during application startup
func InitCFLogger(configDir string) error {
	cfLogMutex.Lock()
	defer cfLogMutex.Unlock()

	cfLogDir = configDir
	logPath := filepath.Join(configDir, cfLogFileName)

	// Check if we need to rotate before opening
	if info, err := os.Stat(logPath); err == nil {
		cfLogSize = info.Size()
		if cfLogSize >= maxLogSize {
			if err := rotateCFLogs(); err != nil {
				return fmt.Errorf("failed to rotate CF logs: %w", err)
			}
			cfLogSize = 0
		}
	}

	// Open log file in append mode
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open CF log file: %w", err)
	}

	cfLogFile = file
	cfLogger = log.New(file, "", log.LstdFlags|log.Lmicroseconds)

	// Log initialization
	logCF("=== CloudFlare Debug Logger Initialized ===")
	logCF("Log file: %s", logPath)
	logCF("Max size: %d MB", maxLogSize/(1024*1024))
	logCF("Max backup files: %d", maxLogFiles)
	logCF("Current log size: %d bytes", cfLogSize)

	return nil
}

// CloseCFLogger closes the CF logger file handle
func CloseCFLogger() {
	cfLogMutex.Lock()
	defer cfLogMutex.Unlock()

	if cfLogFile != nil {
		logCF("=== CloudFlare Debug Logger Closing ===")
		cfLogFile.Close()
		cfLogFile = nil
	}
}

// rotateCFLogs performs log rotation
func rotateCFLogs() error {
	if cfLogFile != nil {
		cfLogFile.Close()
		cfLogFile = nil
	}

	basePath := filepath.Join(cfLogDir, cfLogFileName)

	// Remove oldest backup (cfDebug.log.3)
	oldestBackup := fmt.Sprintf("%s.%d", basePath, maxLogFiles)
	os.Remove(oldestBackup) // Ignore error if file doesn't exist

	// Rotate existing backups
	for i := maxLogFiles - 1; i >= 1; i-- {
		oldPath := fmt.Sprintf("%s.%d", basePath, i)
		newPath := fmt.Sprintf("%s.%d", basePath, i+1)
		os.Rename(oldPath, newPath) // Ignore error if source doesn't exist
	}

	// Move current log to .1
	if err := os.Rename(basePath, basePath+".1"); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

// logCF writes a message to the CF debug log with automatic rotation
func logCF(format string, args ...interface{}) {
	cfLogMutex.Lock()
	defer cfLogMutex.Unlock()

	if cfLogger == nil {
		return // Logger not initialized
	}

	message := fmt.Sprintf(format, args...)

	// Write to log
	cfLogger.Output(2, message)

	// Update size estimate (message + timestamp + newline)
	messageSize := int64(len(message) + 50) // Rough estimate with timestamp
	cfLogSize += messageSize

	// Check if rotation is needed
	if cfLogSize >= maxLogSize {
		if err := rotateCFLogs(); err != nil {
			log.Printf("Failed to rotate CF logs: %v", err)
			return
		}

		// Reopen log file
		logPath := filepath.Join(cfLogDir, cfLogFileName)
		file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Printf("Failed to reopen CF log after rotation: %v", err)
			return
		}

		cfLogFile = file
		cfLogger = log.New(file, "", log.LstdFlags|log.Lmicroseconds)
		cfLogSize = 0

		logCF("=== Log Rotated ===")
	}
}

// LogCFRequest logs details about an outgoing request
func LogCFRequest(domain, url, userAgent string, cookies []string) {
	logCF(">>> OUTGOING REQUEST >>>")
	logCF("  Domain: %s", domain)
	logCF("  URL: %s", url)
	logCF("  User-Agent: %s", userAgent)
	logCF("  Cookies (%d):", len(cookies))
	for i, cookie := range cookies {
		// Truncate cookie values for readability
		if len(cookie) > 100 {
			logCF("    [%d] %s...", i+1, cookie[:100])
		} else {
			logCF("    [%d] %s", i+1, cookie)
		}
	}
	logCF("<<<")
}

// LogCFResponse logs details about a response
func LogCFResponse(statusCode int, bodySize int, headers map[string]string, bodyPreview string) {
	logCF("<<< INCOMING RESPONSE <<<")
	logCF("  Status Code: %d", statusCode)
	logCF("  Body Size: %d bytes", bodySize)
	logCF("  Headers:")
	for key, value := range headers {
		if strings.Contains(strings.ToLower(key), "cookie") ||
			strings.Contains(strings.ToLower(key), "cf-") ||
			strings.Contains(strings.ToLower(key), "server") {
			logCF("    %s: %s", key, value)
		}
	}
	if bodyPreview != "" {
		logCF("  Body Preview (first 500 chars):")
		if len(bodyPreview) > 500 {
			logCF("    %s...", bodyPreview[:500])
		} else {
			logCF("    %s", bodyPreview)
		}
	}
	logCF(">>>")
}

// LogCFDetection logs CF challenge detection details
func LogCFDetection(detected bool, indicators []string, cfInfo *CfInfo) {
	logCF("=== CLOUDFLARE DETECTION ===")
	logCF("  Challenge Detected: %v", detected)
	if detected {
		logCF("  Indicators (%d):", len(indicators))
		for i, indicator := range indicators {
			logCF("    [%d] %s", i+1, indicator)
		}
		if cfInfo != nil {
			logCF("  CF Ray ID: %s", cfInfo.RayID)
			logCF("  Status Code: %d", cfInfo.StatusCode)
			logCF("  Server Header: %s", cfInfo.ServerHeader)
			logCF("  Is BIC: %v", cfInfo.IsBIC)
			logCF("  Turnstile: %v", cfInfo.Turnstile)
			if cfInfo.MetaRedirect != "" {
				logCF("  Meta Redirect: %s", cfInfo.MetaRedirect)
			}
			if cfInfo.FormAction != "" {
				logCF("  Form Action: %s", cfInfo.FormAction)
			}
			logCF("  JS Challenges: %d", len(cfInfo.JSChallenges))
			logCF("  CHL Tokens: %d", len(cfInfo.CHLTokens))
		}
	}
	logCF("===")
}

// LogCFCookieData logs stored cookie bypass data
func LogCFCookieData(domain string, data *BypassData) {
	logCF("=== STORED BYPASS DATA ===")
	logCF("  Domain: %s", domain)
	logCF("  Type: %s", data.Type)
	logCF("  Captured At: %s", data.CapturedAt)

	// Calculate age
	if capturedTime, err := time.Parse(time.RFC3339, data.CapturedAt); err == nil {
		age := time.Since(capturedTime)
		logCF("  Age: %v", age.Round(time.Minute))
	}

	logCF("  Total Cookies: %d", len(data.AllCookies))

	if data.CfClearanceStruct != nil {
		logCF("  CF_CLEARANCE:")
		logCF("    Value: %s", data.CfClearanceStruct.Value)
		logCF("    Domain: %s", data.CfClearanceStruct.Domain)
		logCF("    Path: %s", data.CfClearanceStruct.Path)
		logCF("    HttpOnly: %v", data.CfClearanceStruct.HttpOnly)
		logCF("    Secure: %v", data.CfClearanceStruct.Secure)
		logCF("    SameSite: %s", data.CfClearanceStruct.SameSite)
		if data.CfClearanceStruct.Expires != nil {
			logCF("    Expires: %s", data.CfClearanceStruct.Expires.Format(time.RFC3339))
			timeLeft := time.Until(*data.CfClearanceStruct.Expires)
			if timeLeft < 0 {
				logCF("    ⚠️  EXPIRED %v ago", -timeLeft.Round(time.Minute))
			} else {
				logCF("    Valid for: %v", timeLeft.Round(time.Hour))
			}
		}
		logCF("    Captured At: %s", data.CfClearanceCapturedAt.Format(time.RFC3339))
		logCF("    Captured URL: %s", data.CfClearanceUrl)
	} else {
		logCF("  ⚠️  NO CF_CLEARANCE COOKIE FOUND")
	}

	logCF("  User-Agent: %s", data.Entropy.UserAgent)
	logCF("  Other Cookies:")
	for i, cookie := range data.AllCookies {
		if cookie.Name == "cf_clearance" {
			continue
		}
		logCF("    [%d] %s (domain: %s)", i+1, cookie.Name, cookie.Domain)
	}
	logCF("===")
}

// LogCFImport logs cookie import process
func LogCFImport(domain string, success bool, err error) {
	logCF("=== COOKIE IMPORT ===")
	logCF("  Domain: %s", domain)
	logCF("  Success: %v", success)
	if err != nil {
		logCF("  Error: %v", err)
	}
	logCF("===")
}

// LogCFValidation logs cookie validation results
func LogCFValidation(domain string, valid bool, errors []string) {
	logCF("=== COOKIE VALIDATION ===")
	logCF("  Domain: %s", domain)
	logCF("  Valid: %v", valid)
	if len(errors) > 0 {
		logCF("  Validation Errors:")
		for i, err := range errors {
			logCF("    [%d] %s", i+1, err)
		}
	}
	logCF("===")
}

// LogCFBrowserAction logs browser-based actions
func LogCFBrowserAction(action, url string, cookiesInjected int, success bool, err error) {
	logCF("=== BROWSER ACTION ===")
	logCF("  Action: %s", action)
	logCF("  URL: %s", url)
	logCF("  Cookies Injected: %d", cookiesInjected)
	logCF("  Success: %v", success)
	if err != nil {
		logCF("  Error: %v", err)
	}
	logCF("===")
}

// LogCFError logs CF-related errors
func LogCFError(context, domain string, err error) {
	logCF("!!! ERROR !!!")
	logCF("  Context: %s", context)
	logCF("  Domain: %s", domain)
	logCF("  Error: %v", err)
	logCF("!!!")
}
