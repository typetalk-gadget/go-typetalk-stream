package internal

import (
	"net/http"
	"net/url"
	"sync"

	"github.com/gorilla/websocket"
	"golang.org/x/oauth2"
)

type WSConn struct {
	source oauth2.TokenSource
	conn   *websocket.Conn
	mu     sync.Mutex
}

func (c *WSConn) SetTokenSource(s oauth2.TokenSource) {
	c.source = s
}

func (c *WSConn) Connect() error {

	token, err := c.source.Token()
	if err != nil {
		return err
	}

	u := url.URL{
		Scheme: "wss",
		Host:   "message.typetalk.com",
		Path:   "/api/v1/streaming",
	}
	header := make(http.Header)
	header.Set("Authorization", "Bearer "+token.AccessToken)
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		return NoDialError(err.Error())
	}
	oldConn := c.getConn()
	c.setConn(conn)
	if oldConn != nil {
		_ = oldConn.Close()
	}

	return nil
}

func (c *WSConn) Ping() error {
	err := c.getConn().WriteMessage(websocket.PingMessage, nil)
	if err != nil {
		return NoWriteMessageError(err.Error())
	}
	return nil
}

func (c *WSConn) Read() ([]byte, error) {
	_, data, err := c.getConn().ReadMessage()
	if err != nil {
		return nil, NoReadMessageError(err.Error())
	}
	return data, nil
}

func (c *WSConn) Close() error {
	return c.getConn().Close()
}

func (c *WSConn) getConn() *websocket.Conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn
}

func (c *WSConn) setConn(conn *websocket.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn = conn
}

type WSConnError interface {
	error
	Temporary() bool // Is the error temporary?
}

type NoAccessTokenError string

func (e NoAccessTokenError) Error() string {
	return "no access token " + string(e)
}

func (e NoAccessTokenError) Temporary() bool {
	return false
}

type NoDialError string

func (e NoDialError) Error() string {
	return "no dial " + string(e)
}

func (e NoDialError) Temporary() bool {
	return false
}

type NoWriteMessageError string

func (e NoWriteMessageError) Error() string {
	return "no write message " + string(e)
}

func (e NoWriteMessageError) Temporary() bool {
	return true
}

type NoReadMessageError string

func (e NoReadMessageError) Error() string {
	return "no read message" + string(e)
}

func (e NoReadMessageError) Temporary() bool {
	return true
}
