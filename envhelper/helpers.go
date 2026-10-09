package envhelper

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

func LoadEnv(overload bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if overload {
		return godotenv.Overload(filepath.Join(filepath.Dir(exe), ".env"))
	} else {
		return godotenv.Load(filepath.Join(filepath.Dir(exe), ".env"))
	}
}

func GetEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func GetEnvInt(key string, fallback int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return fallback
}

func GetEnvList(key, delimiter string) []string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return make([]string, 0)
	}

	return strings.Split(v, delimiter)
}
