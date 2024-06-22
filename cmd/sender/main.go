package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

var (
	Version        = "dev"
	defaultTimeout = time.Second * 30
)

func encrypt(key []byte, message []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return append(nonce, aesgcm.Seal(nil, nonce, message, nil)...), nil
}

func publish(ctx context.Context, subject string, body []byte, js jetstream.JetStream, key []byte) (*jetstream.PubAck, error) {
	enc, err := encrypt(key, body)
	if err != nil {
		return nil, fmt.Errorf("can't encrypt: %v", err)
	}
	ack, err := js.Publish(ctx, subject, enc)
	if err != nil {
		return nil, fmt.Errorf("can't publish: %v", err)
	}
	return ack, nil
}

func send(ctx context.Context, targetURL string, body []byte, w http.ResponseWriter, r *http.Request) error {
	proxyReq, err := http.NewRequestWithContext(ctx, r.Method, targetURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "Failed to create request to target", http.StatusBadRequest)
		return fmt.Errorf("Failed to create request: %s - %v", targetURL, err)
	}

	proxyReq.Header = r.Header.Clone()

	client := &http.Client{
		Timeout: defaultTimeout,
	}
	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, "Failed to reach target", http.StatusNotFound)
		return fmt.Errorf("Failed to reach target: %s - %v", targetURL, err)
	}
	defer resp.Body.Close()

	for header, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(header, value)
		}
	}

	w.WriteHeader(resp.StatusCode)
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		http.Error(w, "Failed to copy response body", http.StatusInternalServerError)
		return fmt.Errorf("Failed to copy response body: %s - %v", targetURL, err)
	}
	return nil
}

func mustGetEnv(key, reason string) string {
	value := os.Getenv(key)
	if value == "" {
		log.Fatalf("cannot get %s via \"%s\" from environment", reason, key)
	}
	return value
}

func main() {
	ctx := context.Background()
	log.Printf("Starting %s version %s", os.Args[0], Version)
	addr := mustGetEnv("ADDR", "server address to listen on")
	targetURL := mustGetEnv("TARGET_URL", "target url")
	key := []byte(mustGetEnv("KEY", "end2end encryption key"))
	natsURL := mustGetEnv("NATS_URL", "nats url")
	subject := mustGetEnv("SUBJECT", "nats subject")
	clientName := mustGetEnv("CLIENT_NAME", "client name")
	natsCredentials := mustGetEnv("NATS_CREDENTIALS", "path to nats credentials")

	nc, err := nats.Connect(natsURL, nats.Name(clientName), nats.UserCredentials(natsCredentials), nats.MaxReconnects(-1))
	if err != nil {
		log.Fatal(err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("Error reading body: %v", err)
			http.Error(w, "can't read body", http.StatusBadRequest)
			return
		}

		go func(ctx context.Context, subject string, body []byte, js jetstream.JetStream) {
			ack, err := publish(ctx, subject, body, js, key)
			if err != nil {
				log.Printf("Error publishing: %v", err)
			} else {
				log.Printf("Published to %s @ %s - Sequence: %d", ack.Domain, ack.Stream, ack.Sequence)
			}
		}(ctx, subject, body, js)
		err = send(ctx, targetURL, body, w, r)
		if err != nil {
			log.Printf("Error sending: %v", err)
		}

	})

	log.Println("Listening on", addr)
	err = http.ListenAndServe(addr, mux)
	if err != nil {
		log.Fatal(err)
	}
}
