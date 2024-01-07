package types

type PushRequest struct {
	QueueName string `json:"queue_name"`
	Value     []byte `json:"value"`
}

type PushResponse struct {
	Message string `json:"message"`
}

type Element struct {
	Key   string `json:"key"`
	Value []byte `json:"value"`
}

type PopResponse struct {
	Key   string `json:"key"`
	Value []byte `json:"value"`
}
