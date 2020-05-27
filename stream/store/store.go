package store

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/vvatanabe/expiremap"
)

type AccessToken struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

type TokenStore interface {
	GetAccessToken() (string, error)
}

type MemoryTokenStore struct {
	ClientID     string
	ClientSecret string
	Scope        string
	store        expiremap.Map
}

const (
	accessTokenKey  = "access-token"
	refreshTokenKey = "refresh-token"
)

func (s *MemoryTokenStore) GetAccessToken() (string, error) {

	if v, ok := s.store.Load(accessTokenKey); ok {
		return v.(string), nil
	}

	var (
		t   *AccessToken
		err error
	)
	if v, ok := s.store.Load(refreshTokenKey); ok {
		t, err = refreshAccessToken(s.ClientID, s.ClientSecret, v.(string))
	} else {
		t, err = getAccessToken(s.ClientID, s.ClientSecret, s.Scope)
	}
	if err != nil {
		return "", err
	}

	s.store.StoreWithExpire(accessTokenKey, t.AccessToken, time.Duration(t.ExpiresIn-30)*time.Second)
	s.store.StoreWithExpire(refreshTokenKey, t.RefreshToken, 23*time.Hour+59*time.Minute)

	return t.AccessToken, nil
}

func getAccessToken(clientID, clientSecret, scope string) (*AccessToken, error) {
	form := make(url.Values)
	form.Add("client_id", clientID)
	form.Add("client_secret", clientSecret)
	form.Add("grant_type", "client_credentials")
	form.Add("scope", scope)
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
