package stream

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vvatanabe/go-typetalk-stream/stream/internal"
)

type LoggerFunc func(...interface{})

type Handler interface {
	Serve(message *Message)
}

type HandlerFunc func(msg *Message)

func (f HandlerFunc) Serve(msg *Message) {
	f(msg)
}

const (
	defaultPingInterval = 30 * time.Second
)

type Stream struct {
	ClientID     string
	ClientSecret string
	Handler      Handler
	PingInterval time.Duration
	LoggerFunc   LoggerFunc

	conn internal.WSConn

	started     int32
	inShutdown  int32
	mu          sync.Mutex
	activeMsg   map[*Message]struct{}
	activeMsgWg sync.WaitGroup
	doneChan    chan struct{}
	onShutdown  []func()
}

var (
	ErrStreamSubscribed = errors.New("stream subscribed")
	ErrStreamClosed     = errors.New("stream closed")
)

func (s *Stream) Subscribe() error {

	if atomic.LoadInt32(&s.started) == 1 {
		return ErrStreamSubscribed
	}
	atomic.StoreInt32(&s.started, 1)

	s.conn.SetCredentials(s.ClientID, s.ClientSecret)

	err := s.conn.Connect()
	if err != nil {
		return err
	}
	defer func() {
		_ = s.conn.Close()
	}()

	go func() {
		d := s.PingInterval
		if d < defaultPingInterval {
			d = defaultPingInterval
		}
		t := time.NewTicker(d)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				err := s.conn.Ping()
				if err != nil {
					s.log("ping: ", err)
				}
			}
		}
	}()

	var (
		tempDelay time.Duration // how long to sleep on read failure
	)

	for {
		data, err := s.conn.Read()
		if err != nil {
			select {
			case <-s.getDoneChan():
				return ErrStreamClosed
			default:
			}
			if ne, ok := err.(internal.StreamError); ok && ne.Temporary() {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if max := 1 * time.Second; tempDelay > max {
					tempDelay = max
				}
				s.log("Temporary read error: %v; reconnect in %v", err, tempDelay)
				time.Sleep(tempDelay)
				_ = s.conn.Connect()
				continue
			}
			return err
		}
		tempDelay = 0

		go s.handle(data)
	}
}

func (s *Stream) handle(data []byte) {
	var msg Message
	err := json.Unmarshal(data, &msg)
	if err != nil {
		s.log("invalid received message:", err)
		return
	}
	s.trackMsg(&msg, true)
	defer s.trackMsg(&msg, false)
	s.Handler.Serve(&msg)
}

func (s *Stream) RegisterOnShutdown(f func()) {
	s.mu.Lock()
	s.onShutdown = append(s.onShutdown, f)
	s.mu.Unlock()
}

func (s *Stream) Shutdown(ctx context.Context) error {
	atomic.StoreInt32(&s.inShutdown, 1)

	s.mu.Lock()
	s.closeDoneChanLocked()
	for _, f := range s.onShutdown {
		go f()
	}
	s.mu.Unlock()

	err := s.conn.Close()
	if err != nil {
		s.log("close:", err)
	}

	finished := make(chan struct{}, 1)
	go func() {
		s.activeMsgWg.Wait()
		finished <- struct{}{}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-finished:
		return nil
	}
}

func (s *Stream) shuttingDown() bool {
	return atomic.LoadInt32(&s.inShutdown) != 0
}

func (s *Stream) trackMsg(msg *Message, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeMsg == nil {
		s.activeMsg = make(map[*Message]struct{})
	}
	if add {
		if !s.shuttingDown() {
			s.activeMsg[msg] = struct{}{}
			s.activeMsgWg.Add(1)
		}
	} else {
		delete(s.activeMsg, msg)
		s.activeMsgWg.Done()
	}
}

func (s *Stream) getDoneChan() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getDoneChanLocked()
}

func (s *Stream) getDoneChanLocked() chan struct{} {
	if s.doneChan == nil {
		s.doneChan = make(chan struct{})
	}
	return s.doneChan
}

func (s *Stream) closeDoneChanLocked() {
	ch := s.getDoneChanLocked()
	select {
	case <-ch:
	default:
		close(ch)
	}
}

func (s *Stream) log(args ...interface{}) {
	if s.LoggerFunc != nil {
		args = append([]interface{}{"message: "}, args...)
		s.LoggerFunc(args...)
	}
}

type Message struct {
	Type string `json:"type"`
	Data struct {
		DirectMessage bool `json:"directMessage"`
		Space         struct {
			Key      string `json:"key"`
			Name     string `json:"name"`
			Enabled  bool   `json:"enabled"`
			ImageURL string `json:"imageUrl"`
		} `json:"space"`
		Topic struct {
			ID           int       `json:"id"`
			Name         string    `json:"name"`
			Suggestion   string    `json:"suggestion"`
			LastPostedAt time.Time `json:"lastPostedAt"`
			CreatedAt    time.Time `json:"createdAt"`
			UpdatedAt    time.Time `json:"updatedAt"`
		} `json:"topic"`
		Post struct {
			ID      int         `json:"id"`
			TopicID int         `json:"topicId"`
			ReplyTo interface{} `json:"replyTo"`
			Message string      `json:"message"`
			Account struct {
				ID         int       `json:"id"`
				Name       string    `json:"name"`
				FullName   string    `json:"fullName"`
				Suggestion string    `json:"suggestion"`
				ImageURL   string    `json:"imageUrl"`
				CreatedAt  time.Time `json:"createdAt"`
				UpdatedAt  time.Time `json:"updatedAt"`
			} `json:"account"`
			Mention     interface{}   `json:"mention"`
			Attachments []interface{} `json:"attachments"`
			Likes       []interface{} `json:"likes"`
			Talks       []interface{} `json:"talks"`
			Links       []interface{} `json:"links"`
			CreatedAt   time.Time     `json:"createdAt"`
			UpdatedAt   time.Time     `json:"updatedAt"`
		} `json:"post"`
	} `json:"data"`
}
