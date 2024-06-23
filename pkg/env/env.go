package env

import (
	"log"
	"os"
)

func MustGetEnv(key, reason string) string {
	value := os.Getenv(key)
	if value == "" {
		log.Fatalf("cannot get %s via \"%s\" from environment", reason, key)
	}
	return value
}
