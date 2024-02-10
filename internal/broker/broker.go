package broker

import (
	"Zookeeper/internal/routes"
	"Zookeeper/internal/types"
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

type Client struct {
	Name    string
	Address string
}

func NewBroker(name string, address string) *Client {
	return &Client{Name: name, Address: address}
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
	return nil
}

func processRequest(req *http.Request) ([]byte, error) {
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (b *Client) Do(method, path string, req interface{}, resp interface{}) error {
	var body io.Reader
	if req != nil {
		data, err := json.Marshal(req)
		if err != nil {
			return err
		}
		body = bytes.NewBuffer(data)
	}

	httpRequest, err := b.NewRequest(method, b.Address, path, body)
	if err != nil {
		return err
	}

	data, err := processRequest(httpRequest)
	log.Printf("Response from broker %s: %s", b.Name, string(data))
	if err == nil {
		if resp != nil {
			return json.Unmarshal(data, resp)
		}
		return nil
	}
	return err
}

// Push pushes a message to the queueName
func (b *Client) Push(req *types.Element) error {
	log.Printf("Pushing message to queue %s in broker %s", req.QueueName, b.Name)
	replaceDict := map[string]string{
		"{queue_name}": req.QueueName,
	}
	apiURL := substringReplace(routes.RoutePush, replaceDict)
	request := map[string][]byte{
		"value": req.Value,
	}
	err := b.Do(http.MethodPost, apiURL, request, nil)
	if err != nil {
		return err
	}
	return nil
}

// Front Get front value of any queue that is a master and not empty
func (b *Client) Front() (*types.Element, error) {
	res := &types.Element{}
	err := b.Do(http.MethodGet, routes.RouteFront, nil, res)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// AddQueue adds a queue to the broker
func (b *Client) AddQueue(queueName string, isMaster bool) error {
	log.Printf("Adding queue %s to broker %s", queueName, b.Name)
	req := &types.AddQueueRequest{
		QueueName: queueName,
		IsMaster:  isMaster,
	}
	err := b.Do(http.MethodPost, routes.RouteAddQueue, req, nil)
	return err
}

// Remove pops a message from queue \"queueName\"
func (b *Client) Remove(queueName string) error {
	replaceDict := map[string]string{
		"{queue_name}": queueName,
	}
	apiURL := substringReplace(routes.RoutePop, replaceDict)
	req := map[string]string{
		"queue_name": queueName,
	}
	err := b.Do(http.MethodPost, apiURL, req, nil)
	return err
}

func substringReplace(s string, replaceDict map[string]string) string {
	for k, v := range replaceDict {
		s = strings.Replace(s, k, v, -1)
	}
	return s
}
