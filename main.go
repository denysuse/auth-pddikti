package main

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"auth-pddikti/internal/pddikti"
	"auth-pddikti/internal/server"
)

func main() {
	host := env("HOST", "127.0.0.1")
	port := env("PORT", "8090")

	client := pddikti.NewClient(pddikti.WithTimeout(10 * time.Second))

	router, err := server.NewRouter(client, server.Config{
		APIKey:         os.Getenv("API_KEY"),
		CORSOrigins:    splitList(os.Getenv("CORS_ORIGINS")),
		RatePerMinute:  envInt("RATE_LIMIT_PER_MINUTE", 30),
		MinQueryLen:    envInt("SEARCH_MIN_QUERY_LEN", 3),
		TrustedProxies: splitList(os.Getenv("TRUSTED_PROXIES")),
		EnableSearch:   os.Getenv("ENABLE_SEARCH") == "1",
	})
	if err != nil {
		log.Fatalf("[-] Invalid configuration: %v", err)
	}

	log.Printf("[+] auth-pddikti service running on http://%s:%s", host, port)
	if err := router.Run(host + ":" + port); err != nil {
		log.Fatalf("[-] Failed to run server: %v", err)
	}
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		log.Fatalf("[-] %s harus bilangan bulat >= 0, dapat %q", key, v)
	}
	return n
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
