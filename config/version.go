package config

import (
	"time"
)

var dateString = time.Now().Format("2006-01-02")

var Version = "dev-" + dateString
