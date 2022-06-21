package main

import (
	"agones.dev/agones/pkg/util/signals"
	sdk "agones.dev/agones/sdks/go"
	"context"
	"fmt"
	"github.com/caarlos0/env"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

type config struct {
	HTTP_PORT string `env:"HTTP_PORT" envDefault:"8080"`
}

func main() {
	// Gracefully terminate when signaled to
	go func() {
		ctx := signals.NewSigKillContext()
		<-ctx.Done()
		log.Println("Exit signal. Shutting down.")
		os.Exit(0)
	}()

	// Configuration
	conf, err := setupConfig()
	if err != nil {
		panic(fmt.Errorf("error when calling setupConfig in main: %v", err))
	}
	fmt.Printf("%v", conf.HTTP_PORT)

	// Init director SDK
	log.Print("Creating SDK instance")
	agones, err := sdk.NewSDK()
	if err != nil {
		log.Fatalf("Could not connect to sdk: %v", err)
	}

	// Send health pings to director
	log.Print("Starting to periodically send health pings")
	go func() {
		ctx, _ := context.WithCancel(context.Background()) // TODO: _ is cancel, and should be used by listeners.
		tick := time.Tick(2 * time.Second)
		for {
			log.Printf("Sending a health ping")
			err := agones.Health()
			if err != nil {
				log.Fatalf("Failed to send health ping, %v", err)
			}
			select {
			case <-ctx.Done():
				log.Print("Stopped health pings")
				return
			case <-tick:
			}
		}
	}()

	// TODO: Initialize sockets, continuously listen and serve.
	go tcpListener(conf.HTTP_PORT, agones)

	log.Print("Marking this server as ready")
	if err := agones.Ready(); err != nil {
		log.Fatalf("Could not send ready message")
	}

	// Halt this thread indefinitely
	wg := sync.WaitGroup{}
	wg.Add(1)
	wg.Wait()
}

func tcpListener(port string, agones *sdk.SDK) error {
	log.Printf("Starting socket server, listening on port %s", port)
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("Failed to listen for tcp connections on port %v" + port)
	}
	defer ln.Close()

	es := newEchoServer()
	serv := &http.Server{
		Handler:      es,
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 10,
	}

	// Listen and serve until serving returns an error
	errc := make(chan error, 1)
	go func() {
		agones.Ready()
		errc <- serv.Serve(ln)
	}()

	fmt.Errorf("failed to serve: %v", <-errc)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	return serv.Shutdown(ctx)
}

func setupConfig() (*config, error) {
	conf := &config{}
	err := env.Parse(conf)
	if err != nil {
		return nil, fmt.Errorf("error when parsing environmental variables in setupConfig: %v", err)
	}
	return conf, nil
}
