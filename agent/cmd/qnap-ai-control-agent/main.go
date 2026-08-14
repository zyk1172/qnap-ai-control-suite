package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"qnap-ai-control-suite/agent/internal/api"
	"qnap-ai-control-suite/agent/internal/config"
)

func main() {
	configPath := flag.String("config", envOrDefault("QACS_CONFIG", config.DefaultPath), "config file path")
	printTokenHash := flag.Bool("print-token-hash", false, "read token from stdin and print sha256")
	generateToken := flag.Bool("generate-token", false, "generate an API token")
	flag.Parse()
	if *printTokenHash {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(hashToken(strings.TrimSpace(string(b))))
		return
	}
	if *generateToken {
		token, err := randomToken()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(token)
		return
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := api.New(cfg).Run(api.SignalContext()); err != nil {
		log.Fatal(err)
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
