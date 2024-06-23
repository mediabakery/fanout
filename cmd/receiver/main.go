package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

var (
	Version        = "dev"
	defaultTimeout = time.Second * 30
)

func decrypt(key []byte, encrypted []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return aesgcm.Open(nil, encrypted[:12], encrypted[12:], nil)
}

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

func mustGetEnv(key, reason string) string {
	value := os.Getenv(key)
	if value == "" {
		log.Fatalf("cannot get %s via \"%s\" from environment", reason, key)
	}
	return value
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log.Printf("Starting %s version %s", os.Args[0], Version)
	targetURL := mustGetEnv("TARGET_URL", "target url")
	key := []byte(mustGetEnv("KEY", "end2end encryption key"))
	natsURL := mustGetEnv("NATS_URL", "nats url")
	jsName := mustGetEnv("JS_NAME", "jetstream name")
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

	s, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     jsName,
		Subjects: []string{subject},
		MaxBytes: 1 * 1024 * 1024,
	})
	if err != nil {
		log.Fatal(err)
	}
	c, err := s.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:   clientName,
		AckPolicy: jetstream.AckExplicitPolicy,
	})
	if err != nil {
		log.Fatal(err)
	}
	cons, err := c.Consume(func(msg jetstream.Msg) {
		log.Printf("Received a JetStream message on subject %s", msg.Subject())

		body, err := decrypt(key, msg.Data())
		if err != nil {
			log.Printf("Error decrypting: %v", err)
			msg.Nak()
			return
		}
		err = send(ctx, http.MethodPost, targetURL, body)
		if err != nil {
			log.Printf("Error sending: %v for message", err)
			msg.Nak()
			return
		}
		msg.Ack()
	})
	if err != nil {
		log.Fatal(err)
	}
	defer cons.Stop()
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	fmt.Println("Blocking, press ctrl+c to continue...")
	<-done

}
