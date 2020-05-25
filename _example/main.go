package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vvatanabe/go-typetalk-stream/stream"
)

func main() {
	s := stream.Stream{
		ClientID:     os.Getenv("TYPETALK_CLIENT_ID"),
		ClientSecret: os.Getenv("TYPETALK_CLIENT_SECRET"),
		Handler: stream.HandlerFunc(func(msg *stream.Message) {
			log.Println("message", msg.Type)
		}),
		PingInterval: 30 * time.Second,
		LoggerFunc:   log.Println,
	}

	go func() {
		log.Println("start to subscribe typetalk stream")
		err := s.Subscribe()
		if err != nil {
			log.Println(err)
		}
	}()

	sigint := make(chan os.Signal, 1)
	signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)

	<-sigint

	log.Println("received a signal of graceful shutdown")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	err := s.Shutdown(ctx)
	if err != nil {
		log.Println("failed to graceful shutdown", err)
		return
	}
	log.Println("completed graceful shutdown")
}
