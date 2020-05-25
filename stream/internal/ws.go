package internal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type AccessToken struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

type WSConn struct {
	clientID     string
	clientSecret string

	token      *AccessToken
	expiration time.Time
	conn       *websocket.Conn

	mu sync.RWMutex
}

func (c *WSConn) SetCredentials(clientID, clientSecret string) {
	c.clientID = clientID
	c.clientSecret = clientSecret
}

func (c *WSConn) Connect() error {

	// set access token
	var (
		newToken *AccessToken
		err      error
	)
	if t := c.getToken(); t == nil {
		newToken, err = getAccessToken(c.clientID, c.clientSecret)
	} else {
		newToken, err = refreshAccessToken(c.clientID, c.clientSecret, t.RefreshToken)
	}
	if err != nil {
		return err
	}
	c.setToken(newToken)

	// set ws conn
	u := url.URL{
		Scheme: "wss",
		Host:   "message.typetalk.com",
		Path:   "/api/v1/streaming",
	}
	header := make(http.Header)
	header.Set("Authorization", "Bearer "+c.getToken().AccessToken)
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		return err
	}
	oldConn := c.getConn()
	c.setConn(conn)
	if oldConn != nil {
		_ = oldConn.Close()
	}

	return nil
}

func (c *WSConn) Ping() error {

	if err := c.reconnectIfExpiresSoon(); err != nil {
		return err
	}

	err := c.getConn().WriteMessage(websocket.PingMessage, nil)
	if err != nil {
		return err
	}
	return nil
}

type StreamError interface {
	error
	Temporary() bool // Is the error temporary?
}

type NoReadError string

func (e NoReadError) Error() string {
	return "no read " + string(e)
}

func (e NoReadError) Temporary() bool {
	return true
}

func (c *WSConn) Read() ([]byte, error) {

	if err := c.reconnectIfExpiresSoon(); err != nil {
		return nil, err
	}

	_, data, err := c.getConn().ReadMessage()
	if err != nil {
		return nil, NoReadError(err.Error())
	}
	return data, nil
}

func (c *WSConn) Close() error {
	return c.getConn().Close()
}

type NoReconnectionError string

func (e NoReconnectionError) Error() string {
	return "no reconnection " + string(e)
}

func (e NoReconnectionError) Temporary() bool {
	return false
}

func (c *WSConn) reconnectIfExpiresSoon() error {
	if c.shouldRefreshToken() {
		err := c.Connect()
		if err != nil {
			return NoReconnectionError(err.Error())
		}
	}
	return nil
}

func (c *WSConn) getConn() *websocket.Conn {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn
}

func (c *WSConn) setConn(conn *websocket.Conn) {
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
}

func (c *WSConn) getToken() *AccessToken {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

func (c *WSConn) setToken(token *AccessToken) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = token
	c.expiration = time.Now().Add(time.Duration(token.ExpiresIn-30) * time.Second)
}

func (c *WSConn) shouldRefreshToken() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Now().After(c.expiration)
}

func getAccessToken(clientID, clientSecret string) (*AccessToken, error) {
	form := make(url.Values)
	form.Add("client_id", clientID)
	form.Add("client_secret", clientSecret)
	form.Add("grant_type", "client_credentials")
	form.Add("scope", "topic.read")
	oauth2resp, err := http.PostForm("https://typetalk.com/oauth2/access_token", form)
	if err != nil {
		return nil, fmt.Errorf("credential request error :%w", err)
	}
	var accessToken AccessToken
	err = json.NewDecoder(oauth2resp.Body).Decode(&accessToken)
	if err != nil {
		return nil, fmt.Errorf("credential decode error :%w", err)
	}
	return &accessToken, nil
}

func refreshAccessToken(clientID, clientSecret, refreshToken string) (*AccessToken, error) {
	form := make(url.Values)
	form.Add("client_id", clientID)
	form.Add("client_secret", clientSecret)
	form.Add("grant_type", "refresh_token")
	form.Add("refresh_token", refreshToken)
	oauth2resp, err := http.PostForm("https://typetalk.com/oauth2/access_token", form)
	if err != nil {
		return nil, fmt.Errorf("refresh token error :%w", err)
	}
	var accessToken AccessToken
	err = json.NewDecoder(oauth2resp.Body).Decode(&accessToken)
	if err != nil {
		return nil, fmt.Errorf("credential decode error :%w", err)
	}
	return &accessToken, nil
}
