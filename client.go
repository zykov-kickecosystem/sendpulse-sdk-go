package sendpulse

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type SendpulseError struct {
	HttpCode int
	Url      string
	Body     string
	Message  string
}

func (e *SendpulseError) Error() string {
	return fmt.Sprintf("Http code: %d, url: %s, body: %s, message: %s", e.HttpCode, e.Url, e.Body, e.Message)
}

type client struct {
	config    Config
	token     string
	tokenLock *sync.RWMutex
}

func NewClient(config Config) *client {
	c := &client{config, "", new(sync.RWMutex)}
	return c
}

const apiBaseUrl = "https://api.sendpulse.com"

func (c *client) getToken() (string, error) {
	c.tokenLock.RLock()
	token := c.token
	c.tokenLock.RUnlock()

	if token != "" {
		return token, nil
	}

	data := make(map[string]interface{})
	data["grant_type"] = "client_credentials"
	data["client_id"] = c.config.UserID
	data["client_secret"] = c.config.Secret
	path := "/oauth/access_token"

	body, err := c.makeRequest(path, "POST", data, false)

	if err != nil {
		return "", err
	}

	var respData map[string]interface{}
	if err := json.Unmarshal(body, &respData); err != nil {
		return "", &SendpulseError{http.StatusOK, fmt.Sprintf(apiBaseUrl+"%s", path), string(body), err.Error()}
	}

	accessToken, tokenExists := respData["access_token"]
	if !tokenExists {
		return "", &SendpulseError{http.StatusOK, fmt.Sprintf(apiBaseUrl+"%s", path), string(body), "'access_token' not found in response"}
	}
	accessTokenStr := accessToken.(string)

	c.tokenLock.Lock()
	c.token = accessTokenStr
	token = accessTokenStr
	c.tokenLock.Unlock()

	return token, nil
}

func (c *client) clearToken() {
	c.tokenLock.Lock()
	c.token = ""
	c.tokenLock.Unlock()
}

func (c *client) makeRequest(path string, method string, data map[string]interface{}, useToken bool) ([]byte, error) {
	q := url.Values{}
	for param, value := range data {
		q.Add(param, fmt.Sprintf("%v", value))
	}

	method = strings.ToUpper(method)

	fullPath := apiBaseUrl + path
	req, e := http.NewRequest(method, fullPath, bytes.NewBufferString(q.Encode()))
	if e != nil {
		return nil, e
	}

	if method == "GET" {
		req.URL.RawQuery = q.Encode()
		req.Body = nil
	} else {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; param=value")
	}

	client := &http.Client{
		Timeout: time.Duration(c.config.Timeout) * time.Second,
	}

	if useToken {
		token, err := c.getToken()
		if err != nil {
			return nil, err
		}

		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, &SendpulseError{http.StatusServiceUnavailable, path, "", err.Error()}
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized && useToken {
		c.clearToken()

		respData, err := c.makeRequest(path, method, data, useToken)
		if err != nil {
			return nil, err
		}
		return respData, nil
	}

	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		return nil, &SendpulseError{resp.StatusCode, path, string(body), err.Error()}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &SendpulseError{resp.StatusCode, path, string(body), ""}
	}

	return body, nil
}
