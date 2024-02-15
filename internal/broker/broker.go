package broker

import (
	"Zookeeper/internal/routes"
	"Zookeeper/internal/types"
	"bytes"
	"encoding/json"
	"errors"
	log "github.com/sirupsen/logrus"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Client struct {
	Name    string
	Address string
	Health  bool
	Latency time.Duration
	Mutex   *sync.Mutex
}

func NewBroker(name string, address string) *Client {
	return &Client{
		Name:    name,
		Address: address,
		Health:  false,
		Latency: 0,
		Mutex:   &sync.Mutex{},
	}
}

func (b *Client) NewRequest(method string, url string, route string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url+route, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	return req, nil
}

// HealthCheck checks the health of the broker
func (b *Client) HealthCheck() error {
	start := time.Now()
	err := b.Do(http.MethodGet, "/healthz", 200, nil, nil)
	b.Latency = time.Since(start)
	return err
}

func ensureStatusOK(resp *http.Response, successCode int) error {
	if resp.StatusCode != successCode {
		return errors.New("status code not OK")
	}
	return nil
}

func processRequest(req *http.Request, successCode int) ([]byte, error) {
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)

	err = ensureStatusOK(resp, successCode)

	if err != nil {
		return nil, err
	}
	return data, nil
}

func (b *Client) Do(method, path string, successCode int, req interface{}, resp interface{}) error {
	var body io.Reader
	if req != nil {
		data, err := json.Marshal(req)
		log.Info(string(data))
		if err != nil {
			return err
		}
		body = bytes.NewBuffer(data)
	}

	httpRequest, err := b.NewRequest(method, b.Address, path, body)
	if err != nil {
		return err
	}

	data, err := processRequest(httpRequest, successCode)
	if err == nil {
		if resp != nil {
			return json.Unmarshal(data, resp)
		}
		return nil
	}
	return err
}

// Push pushes a message to the key
func (b *Client) Push(req *types.Element) error {
	b.Mutex.Lock()
	defer b.Mutex.Unlock()

	replaceDict := map[string]string{
		"{key}": req.Key,
	}
	apiURL := substringReplace(routes.RoutePush, replaceDict)
	request := map[string][]byte{
		"value": req.Value,
	}
	err := b.Do(http.MethodPost, apiURL, 200, request, nil)
	if err != nil {
		return err
	}
	return nil
}

// Front Get front value of any key that is a master and not empty
func (b *Client) Front() (*types.Element, error) {
	b.Mutex.Lock()
	defer b.Mutex.Unlock()

	res := &types.Element{}
	err := b.Do(http.MethodGet, routes.RouteFront, 200, nil, res)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// AddKey adds a queue to the broker
func (b *Client) AddKey(key string, isMaster bool) error {
	b.Mutex.Lock()
	defer b.Mutex.Unlock()

	req := &types.AddKeyRequest{
		Key:      key,
		IsMaster: isMaster,
	}
	err := b.Do(http.MethodPost, routes.RouteKey, 200, req, nil)
	return err
}

// Remove pops a message from queue \"queueName\"
func (b *Client) Remove(key string) error {
	b.Mutex.Lock()
	defer b.Mutex.Unlock()

	replaceDict := map[string]string{
		"{key}": key,
	}
	apiURL := substringReplace(routes.RoutePop, replaceDict)
	req := map[string]string{
		"key": key,
	}

	err := b.Do(http.MethodPost, apiURL, 200, req, nil)
	return err
}

func (b *Client) KeySetMaster(key string, masterStatus bool) error {
	b.Mutex.Lock()
	defer b.Mutex.Unlock()

	replaceDict := map[string]string{
		"{key}": key,
	}
	apiURL := substringReplace(routes.RouteMaster, replaceDict)
	req := &types.KeySetMasterRequest{
		MasterStatus: masterStatus,
	}
	err := b.Do(http.MethodPost, apiURL, 200, req, nil)
	return err
}

func (b *Client) Import(key string, isMaster bool, values [][]byte) error {
	req := &types.ImportRequest{
		Key:      key,
		Values:   values,
		IsMaster: isMaster,
	}
	err := b.Do(http.MethodPost, routes.RouteImport, 200, req, nil)
	return err
}

func (b *Client) Export(key string) (*types.ExportResponse, error) {
	req := &types.ExportRequest{
		Key: key,
	}
	res := &types.ExportResponse{}
	err := b.Do(http.MethodGet, routes.RouteExport, 200, req, res)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func substringReplace(s string, replaceDict map[string]string) string {
	for k, v := range replaceDict {
		s = strings.Replace(s, k, v, -1)
	}
	return s
}
