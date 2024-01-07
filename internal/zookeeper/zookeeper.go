package zookeeper

import (
	"Zookeeper/internal/broker"
	"Zookeeper/internal/types"
	"database/sql"
	"github.com/gin-gonic/gin"
)

type Zookeeper struct {
	gin     *gin.Engine
	db      *sql.DB
	brokers map[string]*broker.Client
}

// NewZookeeper returns a new Zookeeper instance
//
// TODO: read brokers from a config file
// TODO: read database connection info from a config file
func NewZookeeper() *Zookeeper {
	conninfo := "user=postgres password=postgres dbname=postgres sslmode=disable"
	db, err := sql.Open("postgres", conninfo)
	if err != nil {
		panic(err)
	}
	gs := &Zookeeper{
		gin: gin.Default(),
		db:  db,
	}
	gs.registerRoutes()
	return gs
}

func (s *Zookeeper) registerRoutes() {
	s.gin.POST("/pop", s.Pop)
	s.gin.POST("/push", s.Push)
}

// Run runs the Zookeeper server
func (s *Zookeeper) Run() {
	s.gin.Run()
}

// Push pushes a message to the queueName
func (s *Zookeeper) Push(c *gin.Context) {
	req := &types.PushRequest{}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	res := &types.PushResponse{}
	if s.GetMasterBroker(req.QueueName) == nil {
		s.AssignQueue(req.QueueName)
	}
	brokers := s.GetBrokers(req.QueueName)
	for _, b := range brokers {
		b.Push(req, res)
	}
	c.JSON(200, gin.H{"message": res.Message})
}

// Pop pops a message
//
// TODO: change logic of removing a message from replicas to event-driven model
func (s *Zookeeper) Pop(c *gin.Context) {
	res := &types.PopResponse{}
	for _, b := range s.brokers {
		b.Pop(res)
		if res.Key != "" {
			s.Erase(res.Key)
			break
		}
	}
	c.JSON(200, gin.H{"key": res.Key, "value": res.Value})
}

// Erase remove a message from queueName. It should be called after a message is popped from queueName
//
// TODO: change logic of removing a message from replicas to event-driven model
func (s *Zookeeper) Erase(queueName string) {
	replicas := s.GetReplicaBrokers(queueName)
	for _, b := range replicas {
		b.Remove(queueName)
	}
}
