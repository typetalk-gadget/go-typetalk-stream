# go-typetalk-stream

go-typetalk-stream is a GO library for using the Typetalk Streaming API.

## Requires

Go 1.14+

## Usage

```go
import "github.com/vvatanabe/go-typetalk-stream/stream"
```

## Example

```go
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
```


## Acknowledgments

Inspired by [net/http](https://golang.org/pkg/net/http/)

## Bugs and Feedback

For bugs, questions and discussions please use the Github Issues.

## License

[MIT License](http://www.opensource.org/licenses/mit-license.php)

## Author

[vvatanabe](https://github.com/vvatanabe)