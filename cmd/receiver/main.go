package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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

func send(ctx context.Context, method string, targetURL string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, method, targetURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %s - %v", targetURL, err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{
		Timeout: defaultTimeout,
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach target: %s - %v", targetURL, err)
	}

	defer resp.Body.Close()
	return nil
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.InfoContext(ctx, fmt.Sprintf("Starting %s", os.Args[0]), "version", Version)
	targetURL := env.MustGetEnv("TARGET_URL", "target url")
	key := []byte(env.MustGetEnv("KEY", "end2end encryption key"))
	natsURL := env.MustGetEnv("NATS_URL", "nats url")
	jsName := env.MustGetEnv("JS_NAME", "jetstream name")
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

	s, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     jsName,
		Subjects: []string{subject},
		MaxBytes: 1 * 1024 * 1024,
	})
	if err != nil {
		logger.ErrorContext(ctx, "Error creating stream", "error", err)
		os.Exit(1)
	}
	c, err := s.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:   clientName,
		AckPolicy: jetstream.AckExplicitPolicy,
	})
	if err != nil {
		logger.ErrorContext(ctx, "Error creating consumer", "error", err)
		os.Exit(1)
	}
	cons, err := c.Consume(func(msg jetstream.Msg) {
		logger.InfoContext(ctx, "Received message", "subject", msg.Subject())

		body, err := e2e.Decrypt(key, msg.Data())
		if err != nil {
			logger.ErrorContext(ctx, "Error decrypting", "error", err)
			msg.Nak()
			return
		}
		err = send(ctx, http.MethodPost, targetURL, body)
		if err != nil {
			logger.ErrorContext(ctx, "Error sending", "error", err)
			msg.Nak()
			return
		}
		msg.Ack()
	})
	if err != nil {
		logger.ErrorContext(ctx, "Error consuming", "error", err)
		os.Exit(1)
	}
	defer cons.Stop()
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	<-done

}
