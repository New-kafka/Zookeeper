package types

type PushRequest struct {
	Key   string `json:"key" binding:"required"`
	Value []byte `json:"value" binding:"required"`
}

type PushResponse struct {
	Message string `json:"message"`
}

type Element struct {
	Key   string `json:"key" binding:"required"`
	Value []byte `json:"value" binding:"required"`
}

type PopResponse struct {
	Key   string `json:"key"`
	Value []byte `json:"value"`
}

type AddQueueRequest struct {
	QueueName string `json:"queue_name"`
	IsMaster  bool   `json:"is_master"`
}
