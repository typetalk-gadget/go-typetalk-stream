package stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/typetalk-gadget/go-typetalk-stream/stream/internal"
	"github.com/typetalk-gadget/go-typetalk-token-source/source"
	"golang.org/x/oauth2"
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
	TokenSource  oauth2.TokenSource
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

	if s.TokenSource != nil {
		s.conn.SetTokenSource(s.TokenSource)
	} else {
		s.conn.SetTokenSource(&source.TokenSource{
			ClientID:     s.ClientID,
			ClientSecret: s.ClientSecret,
			Scope:        "topic.read",
		})
	}

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
					s.log("ping:", err)
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
			if ne, ok := err.(internal.WSConnError); ok && ne.Temporary() {
				if tempDelay == 0 {
					// FIXME
					//tempDelay = 5 * time.Millisecond
					tempDelay = 1 * time.Second
				} else {
					tempDelay *= 2
				}
				if max := 10 * time.Second; tempDelay > max {
					tempDelay = max
				}
				s.log(fmt.Sprintf("temporary read error: %v; reconnect in %v", err, tempDelay))
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
		s.log("no unmarshal message:", err)
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
		s.log("conn close:", err)
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
		args = append([]interface{}{"stream: "}, args...)
		s.LoggerFunc(args...)
	}
}

type Message struct {
	Type string `json:"type"`
	Data Data   `json:"data"`
}

type Data struct {
	DirectMessage *DirectMessage `json:"directMessage"`
	Space         *Space         `json:"space"`
	Topic         *Topic         `json:"topic"`
	Post          *Post          `json:"post"`
}

type DirectMessage struct {
	Account Account `json:"account"`
	Status  Status  `json:"status"`
}

type Account struct {
	ID         int       `json:"id"`
	Name       string    `json:"name"`
	FullName   string    `json:"fullName"`
	Suggestion string    `json:"suggestion"`
	ImageURL   string    `json:"imageUrl"`
	IsBot      bool      `json:"isBot"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type Status struct {
	Presence string      `json:"presence"`
	Web      interface{} `json:"web"`
	Mobile   interface{} `json:"mobile"`
}

type Space struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	ImageURL string `json:"imageUrl"`
}

type Topic struct {
	ID              int       `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	Suggestion      string    `json:"suggestion"`
	IsDirectMessage bool      `json:"isDirectMessage"`
	LastPostedAt    time.Time `json:"lastPostedAt"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type Post struct {
	ID          int              `json:"id"`
	TopicID     int              `json:"topicId"`
	ReplyTo     int              `json:"replyTo"`
	Message     string           `json:"message"`
	Account     Account          `json:"account"`
	Mention     *Mention         `json:"mention"`
	Attachments []AttachmentFile `json:"attachments"`
	Likes       []Like           `json:"likes"`
	Talks       []Talk           `json:"talks"`
	Links       []interface{}    `json:"links"`
	CreatedAt   time.Time        `json:"createdAt"`
	UpdatedAt   time.Time        `json:"updatedAt"`
}

type Mention struct {
	ID     int       `json:"id"`
	ReadAt time.Time `json:"readAt"`
	Post   Post      `json:"post"`
}

type AttachmentFile struct {
	ContentType string `json:"contentType"`
	FileKey     string `json:"fileKey"`
	FileName    string `json:"fileName"`
	FileSize    int    `json:"fileSize"`
}

type Like struct {
	ID        int       `json:"id"`
	PostID    int       `json:"postId"`
	TopicID   int       `json:"topicId"`
	Comment   string    `json:"comment"`
	Account   Account   `json:"account"`
	CreatedAt time.Time `json:"createdAt"`
}

type Talk struct {
	ID         int         `json:"id"`
	TopicID    int         `json:"topicId"`
	Name       string      `json:"name"`
	Suggestion string      `json:"suggestion"`
	CreatedAt  time.Time   `json:"createdAt"`
	UpdatedAt  time.Time   `json:"updatedAt"`
	Backlog    interface{} `json:"backlog"`
}
