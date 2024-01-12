package broker

import "Zookeeper/internal/types"

type Client struct {
	Name    string
	Address string
}

func NewBroker(name string, address string) *Client {
	return &Client{Name: name, Address: address}
}

// HealthCheck checks the health of the broker
func (b *Client) HealthCheck() error {
	return nil
}

// Push pushes a message to the queueName
func (b *Client) Push(req *types.PushRequest, res *types.PushResponse) {

}

// Pop pops a message from a queue which is master in this broker
// and writes the result to res
func (b *Client) Pop(res *types.PopResponse) {
}

// AddQueue adds a queue to the broker
func (b *Client) AddQueue(queueName string, isMaster bool) {
}

// Remove pops a message from queue \"queueName\"
//
// TODO: In case of failure, it should retry it until it succeeds (it can be done by using a channel)
func (b *Client) Remove(queueName string) {

}
