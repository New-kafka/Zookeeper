package types

type PushRequest struct {
	Key   string `json:"key"`
	Value []byte `json:"value"`
}

type PushResponse struct {
	Message string `json:"message"`
}

type Element struct {
	QueueName string `json:"queue_name"`
	Value     []byte `json:"value"`
}

type PopResponse struct {
	Key   string `json:"key"`
	Value []byte `json:"value"`
}

type AddQueueRequest struct {
	QueueName string `json:"queue_name"`
	IsMaster  bool   `json:"is_master"`
}
