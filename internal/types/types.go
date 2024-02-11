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

type ExportRequest struct {
	Key    string   `json:"key" binding:"required"`
	Values [][]byte `json:"values" binding:"required"`
}

type PopResponse struct {
	Key   string `json:"key"`
	Value []byte `json:"value"`
}

type AddKeyRequest struct {
	Key      string `json:"key"`
	IsMaster bool   `json:"isMaster"`
}
