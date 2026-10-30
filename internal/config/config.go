package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Port               string
	GlpiURL            string
	GlpiAppToken       string
	GlpiUserToken      string
	GlpiTechGroup      string
	RefreshIntervalSec int
	MyUserID           int
	MyUserName         string
	DBPath             string
	LogMaxSize         int
	LogMaxAge          int
}

func Load() *Config {
	_ = godotenv.Load()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	glpiURL := strings.TrimRight(os.Getenv("GLPI_URL"), "/")
	if glpiURL == "" {
		glpiURL = "https://grupolipetral.verdanadesk.com"
	}

	appToken := os.Getenv("GLPI_APP_TOKEN")
	userToken := os.Getenv("GLPI_USER_TOKEN")

	techGroup := os.Getenv("GLPI_TECH_GROUP")
	if techGroup == "" {
		techGroup = "1"
	}

	refreshSec := 30
	if val := os.Getenv("REFRESH_INTERVAL_SEC"); val != "" {
		if s, err := strconv.Atoi(val); err == nil && s >= 5 {
			refreshSec = s
		}
	}

	myUserID := 20
	if val := os.Getenv("MY_USER_ID"); val != "" {
		if id, err := strconv.Atoi(val); err == nil && id > 0 {
			myUserID = id
		}
	}

	myUserName := os.Getenv("MY_USER_NAME")
	if myUserName == "" {
		myUserName = "Roberto"
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "data/kanban.db"
	}

	logMaxSize := 50
	if val := os.Getenv("LOG_MAX_SIZE_MB"); val != "" {
		if s, err := strconv.Atoi(val); err == nil && s > 0 {
			logMaxSize = s
		}
	}

	logMaxAge := 30
	if val := os.Getenv("LOG_MAX_AGE_DAYS"); val != "" {
		if a, err := strconv.Atoi(val); err == nil && a > 0 {
			logMaxAge = a
		}
	}

	return &Config{
		Port:               port,
		GlpiURL:            glpiURL,
		GlpiAppToken:       appToken,
		GlpiUserToken:      userToken,
		GlpiTechGroup:      techGroup,
		RefreshIntervalSec: refreshSec,
		MyUserID:           myUserID,
		MyUserName:         myUserName,
		DBPath:             dbPath,
		LogMaxSize:         logMaxSize,
		LogMaxAge:          logMaxAge,
	}
}
