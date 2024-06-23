package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.tilch.dev/fanoutwebhook/pkg/e2e"
	"go.tilch.dev/fanoutwebhook/pkg/env"
)

var (
	Version        = "dev"
	defaultTimeout = time.Second * 30
)

func publish(ctx context.Context, subject string, body []byte, js jetstream.JetStream, key []byte) (*jetstream.PubAck, error) {
	enc, err := e2e.Encrypt(key, body)
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
	req, err := http.NewRequestWithContext(ctx, r.Method, targetURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "Failed to create request to target", http.StatusBadRequest)
		return fmt.Errorf("failed to create request: %s - %v", targetURL, err)
	}

	req.Header = r.Header.Clone()
	client := &http.Client{
		Timeout: defaultTimeout,
	}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Failed to reach target", http.StatusNotFound)
		return fmt.Errorf("failed to reach target: %s - %v", targetURL, err)
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
		return fmt.Errorf("failed to copy response body: %s - %v", targetURL, err)
	}
	return nil
}

func main() {
	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	logger.InfoContext(ctx, fmt.Sprintf("Starting %s", os.Args[0]), "version", Version)
	addr := env.MustGetEnv("ADDR", "server address to listen on")
	targetURL := env.MustGetEnv("TARGET_URL", "target url")
	key := []byte(env.MustGetEnv("KEY", "end2end encryption key"))
	natsURL := env.MustGetEnv("NATS_URL", "nats url")
	subject := env.MustGetEnv("SUBJECT", "nats subject")
	clientName := env.MustGetEnv("CLIENT_NAME", "client name")
	natsCredentials := env.MustGetEnv("NATS_CREDENTIALS", "path to nats credentials")

	nc, err := nats.Connect(natsURL, nats.Name(clientName), nats.UserCredentials(natsCredentials), nats.MaxReconnects(-1))
	if err != nil {
		logger.ErrorContext(ctx, "Error connecting to nats", "error", err)
		os.Exit(1)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		logger.ErrorContext(ctx, "Error connecting to jetstream", "error", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			logger.ErrorContext(ctx, "Error reading body", "error", err)
			http.Error(w, "can't read body", http.StatusBadRequest)
			return
		}

		go func(ctx context.Context, subject string, body []byte, js jetstream.JetStream) {
			ack, err := publish(ctx, subject, body, js, key)
			if err != nil {
				logger.ErrorContext(ctx, "Error publishing", "error", err)
			} else {
				logger.InfoContext(ctx, "Published", "domain", ack.Domain, "stream", ack.Stream, "sequence", ack.Sequence)
			}
		}(ctx, subject, body, js)
		err = send(ctx, targetURL, body, w, r)
		if err != nil {
			logger.ErrorContext(ctx, "Error sending", "error", err)
		}

	})

	logger.InfoContext(ctx, "Listening on", "address", addr)
	err = http.ListenAndServe(addr, mux)
	if err != nil {
		logger.ErrorContext(ctx, "Error listening", "error", err)
		os.Exit(1)
	}
}
